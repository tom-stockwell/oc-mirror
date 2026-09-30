package additional

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/openshift/oc-mirror/v2/internal/pkg/api/v2alpha1"
	"github.com/openshift/oc-mirror/v2/internal/pkg/consts"
	"github.com/openshift/oc-mirror/v2/internal/pkg/image"
	clog "github.com/openshift/oc-mirror/v2/internal/pkg/log"
	"github.com/openshift/oc-mirror/v2/internal/pkg/manifest"
	"github.com/openshift/oc-mirror/v2/internal/pkg/mirror"
)

// sigTagPattern matches the cosign/sigstore signature-tag convention
// (sha256-<digest>.sig) so it can always be excluded from tag-regex matches:
// oc-mirror already mirrors an image's signature automatically alongside the
// image itself, so matching it directly as a standalone image is redundant
// and would likely fail since it isn't a regular image manifest.
var sigTagPattern = regexp.MustCompile(`^sha256-[0-9a-fA-F]{64}\.sig$`)

type LocalStorageCollector struct {
	Log                clog.PluggableLoggerInterface
	Mirror             mirror.MirrorInterface
	Manifest           manifest.ManifestInterface
	Config             v2alpha1.ImageSetConfiguration
	Opts               mirror.CopyOptions
	LocalStorageFQDN   string
	destReg            string
	generateV1DestTags bool
}

func WithV1Tags(o CollectorInterface) CollectorInterface {
	switch impl := o.(type) {
	case *LocalStorageCollector:
		impl.generateV1DestTags = true
	}
	return o
}

func (o LocalStorageCollector) destinationRegistry() string {
	if o.destReg == "" {
		if o.Opts.Mode == mirror.DiskToMirror || o.Opts.Mode == mirror.MirrorToMirror {
			o.destReg = strings.TrimPrefix(o.Opts.Destination, consts.DockerProtocol)
		} else {
			o.destReg = o.LocalStorageFQDN
		}
	}
	return o.destReg
}

// AdditionalImagesCollector - this looks into the additional images field
// taking into account the mode we are in (mirrorToDisk, diskToMirror)
// the image is downloaded in oci format
func (o LocalStorageCollector) AdditionalImagesCollector(ctx context.Context) (v2alpha1.CollectorSchema, error) {
	var allImages []v2alpha1.CopyImageSchema
	var allErrs []error
	platformFilters := make(map[string][]v2alpha1.InstancePlatformFilter)

	expanded, expandErrs := o.expandTagsByRegexImages(ctx, o.Config.ImageSetConfigurationSpec.Mirror.AdditionalImages)
	allErrs = append(allErrs, expandErrs...)

	o.Log.Debug(collectorPrefix+"setting copy option o.Opts.MultiArch=%s when collecting releases image", o.Opts.MultiArch)
	for _, img := range expanded {
		src, dest, origin, err := o.resolveAdditionalImageSrcDest(img)
		if err != nil {
			allErrs = append(allErrs, err)
			continue
		}
		o.Log.Debug(collectorPrefix+"source %s", src)
		o.Log.Debug(collectorPrefix+"destination %s", dest)

		allImages = append(allImages, v2alpha1.CopyImageSchema{
			Source:      src,
			Destination: dest,
			Origin:      origin,
			Type:        v2alpha1.TypeGeneric,
		})
		if err := v2alpha1.ValidatePlatforms(img.Platforms); err != nil {
			allErrs = append(allErrs, fmt.Errorf("invalid platform for image %q: %w", img.Name, err))
		} else if len(img.Platforms) > 0 {
			platformFilters[origin] = img.Platforms
		}
	}
	cs := v2alpha1.CollectorSchema{AllImages: allImages, PlatformFilters: platformFilters}
	return cs, errors.Join(allErrs...)
}

// resolveAdditionalImageSrcDest validates an additional image entry and returns its
// resolved source, destination and origin references ready for mirroring.
func (o LocalStorageCollector) resolveAdditionalImageSrcDest(img v2alpha1.AdditionalImage) (src, dest, origin string, err error) {
	origin = img.Name

	imgSpec, err := image.ParseRef(img.Name)
	if err != nil {
		// OCPBUGS-33081 - skip if parse error (i.e semver and other)
		o.Log.Warn("%v : SKIPPING", err)
		return "", "", "", fmt.Errorf("parse image %q: %w", img.Name, err)
	}

	if img.TargetRepo != "" && !v2alpha1.IsValidPathComponent(img.TargetRepo) {
		o.Log.Warn("invalid targetRepo %s for image %s : SKIPPING", img.TargetRepo, img.Name)
		return "", "", "", fmt.Errorf("invalid targetRepo %s for image %s", img.TargetRepo, img.Name)
	}

	targetRepo, targetTag := resolveTargetRepoTag(img, imgSpec)

	var tmpSrc, tmpDest string
	switch {
	case o.Opts.IsMirrorToDisk(), o.Opts.IsMirrorToMirror():
		tmpSrc, tmpDest = o.buildMirrorToDiskPaths(img, imgSpec, targetRepo, targetTag)
	case o.Opts.IsDiskToMirror(), o.Opts.IsDelete():
		tmpSrc, tmpDest = o.buildDiskToMirrorPaths(img, imgSpec, targetRepo, targetTag)
	}

	if tmpSrc == "" || tmpDest == "" {
		o.Log.Error(collectorPrefix+"unable to determine src %s or dst %s for %s", tmpSrc, tmpDest, img.Name)
		return "", "", "", fmt.Errorf("unable to determine src %s or dst %s for %s", tmpSrc, tmpDest, img.Name)
	}

	srcSpec, err := image.ParseRef(tmpSrc)
	if err != nil {
		o.Log.Error(errMsg, err.Error())
		return "", "", "", fmt.Errorf("parse source %q: %w", tmpSrc, err)
	}
	destSpec, err := image.ParseRef(tmpDest)
	if err != nil {
		o.Log.Error(errMsg, err.Error())
		return "", "", "", fmt.Errorf("parse destination %q: %w", tmpDest, err)
	}
	return srcSpec.ReferenceWithTransport, destSpec.ReferenceWithTransport, origin, nil
}

// buildMirrorToDiskPaths constructs source and destination paths for mirror-to-disk and mirror-to-mirror operations
func (o LocalStorageCollector) buildMirrorToDiskPaths(img v2alpha1.AdditionalImage, imgSpec image.ImageSpec, targetRepo, targetTag string) (string, string) {
	tmpSrc := imgSpec.ReferenceWithTransport
	var tmpDest string

	if imgSpec.Transport != consts.DockerProtocol {
		// oci image
		// Although fetching the digest of the oci image (using o.Manifest.GetDigest) might work in mirrorToDisk and mirrorToMirror
		// it will not work during diskToMirror as the oci image might not be on the disk any longer
		tag := latestTag
		if img.TargetTag != "" {
			tag = targetTag
		}
		tmpDest = fmt.Sprintf("%s/%s:%s", o.destinationRegistry(), strings.TrimPrefix(targetRepo, "/"), tag)
		return tmpSrc, tmpDest
	}

	// Docker protocol
	switch {
	case imgSpec.IsImageByTagAndDigest():
		// OCPBUGS-33196 + OCPBUGS-37867- check source image for tag and digest
		// use tag only for both src and dest
		o.Log.Warn(collectorPrefix+"%s has both tag and digest : using digest to pull, but tag only for mirroring", imgSpec.Reference)
		tmpSrc = fmt.Sprintf("%s/%s@%s:%s", imgSpec.Domain, imgSpec.PathComponent, imgSpec.Algorithm, imgSpec.Digest)
		tmpDest = fmt.Sprintf("%s/%s:%s", o.destinationRegistry(), targetRepo, targetTag)
	case imgSpec.IsImageByDigestOnly() && img.TargetTag == "":
		tmpDest = fmt.Sprintf("%s/%s:%s-%s", o.destinationRegistry(), targetRepo, imgSpec.Algorithm, imgSpec.Digest)
	default:
		tmpDest = fmt.Sprintf("%s/%s:%s", o.destinationRegistry(), targetRepo, targetTag)
	}

	return tmpSrc, tmpDest
}

// buildDiskToMirrorPaths constructs source and destination paths for disk-to-mirror operations
func (o LocalStorageCollector) buildDiskToMirrorPaths(img v2alpha1.AdditionalImage, imgSpec image.ImageSpec, targetRepo, targetTag string) (string, string) {
	// Docker protocol
	var tmpSrc, tmpDest string

	if imgSpec.Transport != consts.DockerProtocol {
		// oci image
		tag := latestTag
		if img.TargetTag != "" {
			tag = targetTag
		}
		tmpSrc = fmt.Sprintf("%s/%s:%s", o.LocalStorageFQDN, strings.TrimPrefix(targetRepo, "/"), tag)
		tmpDest = fmt.Sprintf("%s/%s:%s", o.Opts.Destination, strings.TrimPrefix(targetRepo, "/"), tag)
		return tmpSrc, tmpDest
	}

	switch {
	case imgSpec.IsImageByDigestOnly() && img.TargetTag == "" && o.generateV1DestTags:
		tmpSrc = fmt.Sprintf("%s/%s:%s-%s", o.LocalStorageFQDN, targetRepo, imgSpec.Algorithm, imgSpec.Digest)
		tmpDest = fmt.Sprintf("%s/%s:%s", o.Opts.Destination, targetRepo, latestTag)
	case imgSpec.IsImageByDigestOnly() && img.TargetTag == "":
		digestTag := imgSpec.Algorithm + "-" + imgSpec.Digest
		tmpSrc = fmt.Sprintf("%s/%s:%s", o.LocalStorageFQDN, targetRepo, digestTag)
		tmpDest = fmt.Sprintf("%s/%s:%s", o.Opts.Destination, targetRepo, digestTag)
	default:
		// OCPBUGS-33196 + OCPBUGS-37867- check source image for tag and digest
		if imgSpec.IsImageByTagAndDigest() {
			o.Log.Warn(collectorPrefix+"%s has both tag and digest : using tag only", imgSpec.Reference)
		}
		tmpSrc = fmt.Sprintf("%s/%s:%s", o.LocalStorageFQDN, targetRepo, targetTag)
		tmpDest = fmt.Sprintf("%s/%s:%s", o.Opts.Destination, targetRepo, targetTag)
	}

	return tmpSrc, tmpDest
}

// resolveTargetRepoTag returns the effective target repository and tag for an additional image,
// applying any overrides from the image configuration.
func resolveTargetRepoTag(img v2alpha1.AdditionalImage, imgSpec image.ImageSpec) (string, string) {
	targetRepo := imgSpec.PathComponent
	if img.TargetRepo != "" {
		targetRepo = img.TargetRepo
	}
	targetTag := imgSpec.Tag
	if img.TargetTag != "" {
		targetTag = img.TargetTag
	}
	return targetRepo, targetTag
}

// expandTagsByRegexImages replaces every AdditionalImage entry that has
// TagsByRegex set with one concrete AdditionalImage per tag in its
// repository that matches the regex (skipping cosign signature tags),
// leaving entries without TagsByRegex untouched. Expanded entries are then
// resolved by the existing, unmodified per-tag resolution logic.
//
// Entries with TagsByRegex set require a registry (or on-disk cache) round
// trip per repo via tagsForRepo; since those are independent of each other,
// they are resolved concurrently, bounded by CopyOptions.ParallelImages (the
// same knob that bounds concurrent image copies elsewhere), to avoid the
// fixed per-call registry handshake overhead compounding linearly across
// many entries. Each input entry writes only to its own result slot, so no
// mutex is needed and output order matches input order.
func (o LocalStorageCollector) expandTagsByRegexImages(ctx context.Context, in []v2alpha1.AdditionalImage) ([]v2alpha1.AdditionalImage, []error) {
	results := make([][]v2alpha1.AdditionalImage, len(in))
	errsByIdx := make([]error, len(in))

	// ParallelImages is normally defaulted/validated to a value >= 1 by the
	// CLI flag handling; guard against an unset (zero) value here only to
	// avoid blocking forever on an empty semaphore, not to pick a parallelism
	// of our own.
	parallelism := o.Opts.ParallelImages
	if parallelism == 0 {
		parallelism = 1
	}
	semaphore := make(chan struct{}, parallelism)

	var wg sync.WaitGroup
	for i, img := range in {
		if img.TagsByRegex == "" {
			results[i] = []v2alpha1.AdditionalImage{img}
			continue
		}

		semaphore <- struct{}{}
		wg.Add(1)
		go func(idx int, img v2alpha1.AdditionalImage) {
			defer wg.Done()
			defer func() { <-semaphore }()
			results[idx], errsByIdx[idx] = o.expandTagsByRegexImage(ctx, img)
		}(i, img)
	}
	wg.Wait()

	var out []v2alpha1.AdditionalImage
	var errs []error
	for i := range in {
		out = append(out, results[i]...)
		if errsByIdx[i] != nil {
			errs = append(errs, errsByIdx[i])
		}
	}

	return out, errs
}

// expandTagsByRegexImage resolves a single AdditionalImage entry that has
// TagsByRegex set into one concrete AdditionalImage per matching tag.
func (o LocalStorageCollector) expandTagsByRegexImage(ctx context.Context, img v2alpha1.AdditionalImage) ([]v2alpha1.AdditionalImage, error) {
	imgSpec, err := image.ParseBareRepo(img.Name)
	if err != nil {
		return nil, fmt.Errorf("additional image %q: %w", img.Name, err)
	}
	repo := imgSpec.Name

	re, err := regexp.Compile(img.TagsByRegex)
	if err != nil {
		return nil, fmt.Errorf("additional image %q: invalid tagsByRegex %q: %w", img.Name, img.TagsByRegex, err)
	}

	tags, err := o.tagsForRepo(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("additional image %q: %w", img.Name, err)
	}

	var out []v2alpha1.AdditionalImage
	for _, tag := range tags {
		if sigTagPattern.MatchString(tag) {
			continue
		}
		if !re.MatchString(tag) {
			continue
		}
		out = append(out, v2alpha1.AdditionalImage{
			Name:       repo + ":" + tag,
			TargetRepo: img.TargetRepo,
			Platforms:  img.Platforms,
		})
	}
	if len(out) == 0 {
		o.Log.Warn(collectorPrefix+"no tags in %q matched tagsByRegex %q", repo, img.TagsByRegex)
	}

	return out, nil
}

// tagsForRepo returns the list of tags for repo, mode-gated the same way the
// Cincinnati graph-data cache is: mirrorToDisk/mirrorToMirror query the
// registry live (mirrorToDisk additionally caches the result to
// working-dir/ so diskToMirror can replay it), while diskToMirror/delete
// read that cache only - they must not re-query the registry, since it may
// be unreachable in a disconnected environment and tags may have drifted
// since mirrorToDisk.
func (o LocalStorageCollector) tagsForRepo(ctx context.Context, repo string) ([]string, error) {
	if o.Opts.IsDiskToMirror() || o.Opts.IsDelete() {
		meta, err := loadRepoMetadata(o.Opts.Global.WorkingDir, repo)
		if err != nil {
			return nil, err
		}
		return meta.Tags, nil
	}

	sysCtx, err := o.Opts.SrcImage.NewSystemContext()
	if err != nil {
		return nil, fmt.Errorf("get system context for %q: %w", repo, err)
	}
	tags, err := o.Manifest.GetRepositoryTags(ctx, sysCtx, repo)
	if err != nil {
		return nil, fmt.Errorf("list tags for %q: %w", repo, err)
	}

	if o.Opts.IsMirrorToDisk() {
		if err := writeRepoMetadata(o.Opts.Global.WorkingDir, repo, repoMetadata{Tags: tags}); err != nil {
			return nil, err
		}
	}

	return tags, nil
}
