package additional

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	specv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/types"

	"github.com/openshift/oc-mirror/v2/internal/pkg/api/v2alpha1"
	"github.com/openshift/oc-mirror/v2/internal/pkg/consts"
	clog "github.com/openshift/oc-mirror/v2/internal/pkg/log"
	"github.com/openshift/oc-mirror/v2/internal/pkg/mirror"
)

// setup mocks
// we need to mock Manifest, Mirror

type (
	MockMirror   struct{}
	MockManifest struct {
		Log clog.PluggableLoggerInterface
		// RepoTags, when non-nil, maps a repository reference to the tag
		// list GetRepositoryTags should return for it. Used by tests that
		// exercise TagsByRegex expansion.
		RepoTags map[string][]string
		// RepoTagsCalls counts GetRepositoryTags invocations, so tests can
		// assert it is never called (e.g. in diskToMirror/delete mode).
		// Atomic since TagsByRegex entries may be resolved concurrently.
		RepoTagsCalls *atomic.Int64
	}
)

func TestAdditionalImageCollector(t *testing.T) {
	log := clog.New("trace")

	global := &mirror.GlobalOptions{SecurePolicy: false}
	_, sharedOpts := mirror.SharedImageFlags()
	_, deprecatedTLSVerifyOpt := mirror.DeprecatedTLSVerifyFlags()
	_, srcOpts := mirror.ImageSrcFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "src-", "screds")
	_, destOpts := mirror.ImageDestFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "dest-", "dcreds")
	_, retryOpts := mirror.RetryFlags()

	localstorageFQDN := "test.registry.com"

	opts := mirror.CopyOptions{
		Global:              global,
		DeprecatedTLSVerify: deprecatedTLSVerifyOpt,
		SrcImage:            srcOpts,
		DestImage:           destOpts,
		RetryOpts:           retryOpts,
		Destination:         consts.OciProtocol + "test",
		Dev:                 false,
		Mode:                mirror.MirrorToDisk,
		LocalStorageFQDN:    localstorageFQDN,
	}

	// use the minamal amount of images
	// simplifies testing
	cfg := v2alpha1.ImageSetConfiguration{
		ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
			Mirror: v2alpha1.Mirror{
				AdditionalImages: []v2alpha1.AdditionalImage{
					{Name: "registry.redhat.io/ubi8/ubi:latest"},
					{Name: "registry.redhat.io/ubi8/ubi:latest@sha256:44d75007b39e0e1bbf1bcfd0721245add54c54c3f83903f8926fb4bef6827aa2"},
					{Name: "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea"},
					{Name: "oci:///folder-a/folder-b/testns/test"},
				},
			},
		},
	}

	mockmirror := MockMirror{}
	manifest := MockManifest{Log: log}

	ex := New(log, cfg, opts, mockmirror, manifest)
	ctx := context.Background()

	// this test covers mirrorToDisk
	t.Run("Testing AdditionalImagesCollector : mirrorToDisk should pass", func(t *testing.T) {
		expected := []v2alpha1.CopyImageSchema{
			{
				Source:      consts.DockerProtocol + "registry.redhat.io/ubi8/ubi:latest",
				Origin:      "registry.redhat.io/ubi8/ubi:latest",
				Destination: consts.DockerProtocol + "test.registry.com/ubi8/ubi:latest",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Source:      consts.DockerProtocol + "registry.redhat.io/ubi8/ubi@sha256:44d75007b39e0e1bbf1bcfd0721245add54c54c3f83903f8926fb4bef6827aa2",
				Origin:      "registry.redhat.io/ubi8/ubi:latest@sha256:44d75007b39e0e1bbf1bcfd0721245add54c54c3f83903f8926fb4bef6827aa2",
				Destination: consts.DockerProtocol + "test.registry.com/ubi8/ubi:latest",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Source:      consts.DockerProtocol + "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Origin:      "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Destination: consts.DockerProtocol + "test.registry.com/testns/test:sha256-f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Source:      "oci:///folder-a/folder-b/testns/test",
				Origin:      "oci:///folder-a/folder-b/testns/test",
				Destination: consts.DockerProtocol + "test.registry.com/folder-a/folder-b/testns/test:latest",
				Type:        v2alpha1.TypeGeneric,
			},
		}
		res, err := ex.AdditionalImagesCollector(ctx)
		if err != nil {
			log.Error(" %v ", err)
			t.Fatalf("should not fail")
		}
		assert.ElementsMatch(t, expected, res.AllImages)
	})

	// update opts
	// this test covers diskToMirror
	opts.Mode = mirror.DiskToMirror
	opts.Destination = consts.DockerProtocol + "mirror.acme.com"
	ex = New(log, cfg, opts, mockmirror, manifest)

	t.Run("Testing AdditionalImagesCollector : diskToMirror should pass", func(t *testing.T) {
		expected := []v2alpha1.CopyImageSchema{
			{
				Destination: consts.DockerProtocol + "mirror.acme.com/ubi8/ubi:latest",
				Origin:      "registry.redhat.io/ubi8/ubi:latest",
				Source:      consts.DockerProtocol + "test.registry.com/ubi8/ubi:latest",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Destination: consts.DockerProtocol + "mirror.acme.com/ubi8/ubi:latest",
				Origin:      "registry.redhat.io/ubi8/ubi:latest@sha256:44d75007b39e0e1bbf1bcfd0721245add54c54c3f83903f8926fb4bef6827aa2",
				Source:      consts.DockerProtocol + "test.registry.com/ubi8/ubi:latest",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Destination: consts.DockerProtocol + "mirror.acme.com/testns/test:sha256-f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Origin:      "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Source:      consts.DockerProtocol + "test.registry.com/testns/test:sha256-f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Destination: consts.DockerProtocol + "mirror.acme.com/folder-a/folder-b/testns/test:latest",
				Origin:      "oci:///folder-a/folder-b/testns/test",
				Source:      consts.DockerProtocol + "test.registry.com/folder-a/folder-b/testns/test:latest",
				Type:        v2alpha1.TypeGeneric,
			},
		}
		res, err := ex.AdditionalImagesCollector(ctx)
		if err != nil {
			log.Error(" %v ", err)
			t.Fatalf("should not fail")
		}
		assert.ElementsMatch(t, expected, res.AllImages)
	})

	t.Run("Testing AdditionalImagesCollector : diskToMirror with generateV1Tags should use latest for images by digest", func(t *testing.T) {
		// should error diskToMirror
		opts.Mode = mirror.DiskToMirror
		ex = New(log, cfg, opts, mockmirror, manifest)
		ex = WithV1Tags(ex)
		expected := []v2alpha1.CopyImageSchema{
			{
				Destination: consts.DockerProtocol + "mirror.acme.com/ubi8/ubi:latest",
				Origin:      "registry.redhat.io/ubi8/ubi:latest",
				Source:      consts.DockerProtocol + "test.registry.com/ubi8/ubi:latest",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Destination: consts.DockerProtocol + "mirror.acme.com/ubi8/ubi:latest",
				Origin:      "registry.redhat.io/ubi8/ubi:latest@sha256:44d75007b39e0e1bbf1bcfd0721245add54c54c3f83903f8926fb4bef6827aa2",
				Source:      consts.DockerProtocol + "test.registry.com/ubi8/ubi:latest",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Destination: consts.DockerProtocol + "mirror.acme.com/testns/test:latest",
				Origin:      "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Source:      consts.DockerProtocol + "test.registry.com/testns/test:sha256-f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Destination: consts.DockerProtocol + "mirror.acme.com/folder-a/folder-b/testns/test:latest",
				Origin:      "oci:///folder-a/folder-b/testns/test",
				Source:      consts.DockerProtocol + "test.registry.com/folder-a/folder-b/testns/test:latest",
				Type:        v2alpha1.TypeGeneric,
			},
		}
		res, err := ex.AdditionalImagesCollector(ctx)
		if err != nil {
			log.Error(" %v ", err)
			t.Fatalf("should not fail")
		}
		assert.ElementsMatch(t, expected, res.AllImages)
	})

	// should error mirrorToDisk
	cfg.Mirror.AdditionalImages[1].Name = "sometest.registry.com/testns/test@shaf30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea"
	opts.Mode = mirror.MirrorToDisk
	ex = New(log, cfg, opts, mockmirror, manifest)

	t.Run("Testing AdditionalImagesCollector : mirrorToDisk should collect valid images and return parse errors", func(t *testing.T) {
		expected := []v2alpha1.CopyImageSchema{
			{
				Source:      consts.DockerProtocol + "registry.redhat.io/ubi8/ubi:latest",
				Origin:      "registry.redhat.io/ubi8/ubi:latest",
				Destination: consts.DockerProtocol + "test.registry.com/ubi8/ubi:latest",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Source:      consts.DockerProtocol + "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Origin:      "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Destination: consts.DockerProtocol + "test.registry.com/testns/test:sha256-f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Source:      "oci:///folder-a/folder-b/testns/test",
				Origin:      "oci:///folder-a/folder-b/testns/test",
				Destination: consts.DockerProtocol + "test.registry.com/folder-a/folder-b/testns/test:latest",
				Type:        v2alpha1.TypeGeneric,
			},
		}
		res, err := ex.AdditionalImagesCollector(ctx)
		// Should return error for images that failed to parse
		require.Error(t, err)
		assert.ElementsMatch(t, expected, res.AllImages)
	})

	t.Run("Testing AdditionalImagesCollector : diskToMirror should collect valid images and return parse errors", func(t *testing.T) {
		// should error diskToMirror
		cfg.Mirror.AdditionalImages[1].Name = "sometest.registry.com/testns/test@shaf30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea"
		opts.Mode = mirror.DiskToMirror
		ex = New(log, cfg, opts, mockmirror, manifest)
		expected := []v2alpha1.CopyImageSchema{
			{
				Destination: consts.DockerProtocol + "mirror.acme.com/ubi8/ubi:latest",
				Origin:      "registry.redhat.io/ubi8/ubi:latest",
				Source:      consts.DockerProtocol + "test.registry.com/ubi8/ubi:latest",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Destination: consts.DockerProtocol + "mirror.acme.com/testns/test:sha256-f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Origin:      "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Source:      consts.DockerProtocol + "test.registry.com/testns/test:sha256-f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Destination: consts.DockerProtocol + "mirror.acme.com/folder-a/folder-b/testns/test:latest",
				Origin:      "oci:///folder-a/folder-b/testns/test",
				Source:      consts.DockerProtocol + "test.registry.com/folder-a/folder-b/testns/test:latest",
				Type:        v2alpha1.TypeGeneric,
			},
		}
		res, err := ex.AdditionalImagesCollector(ctx)
		// Should return error for images that failed to parse
		require.Error(t, err)
		assert.ElementsMatch(t, expected, res.AllImages)
	})
}

func TestAdditionalImageCollectorWithTargetRepoAndTag(t *testing.T) {
	type spec struct {
		name             string
		mode             string
		destination      string
		additionalImages []v2alpha1.AdditionalImage
		useV1Tags        bool
		expected         []v2alpha1.CopyImageSchema
		expectError      bool
		errContains      []string
	}

	cases := []spec{
		{
			name:        "mirrorToDisk with TargetRepo",
			mode:        mirror.MirrorToDisk,
			destination: "oci://test",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:       "registry.redhat.io/ubi8/ubi:latest",
					TargetRepo: "custom-namespace/custom-image",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://registry.redhat.io/ubi8/ubi:latest",
					Origin:      "registry.redhat.io/ubi8/ubi:latest",
					Destination: "docker://test.registry.com/custom-namespace/custom-image:latest",
					Type:        v2alpha1.TypeGeneric,
				},
			},
		},
		{
			name:        "mirrorToDisk with TargetTag",
			mode:        mirror.MirrorToDisk,
			destination: "oci://test",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:      "registry.redhat.io/ubi8/ubi:latest",
					TargetTag: "v1.0",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://registry.redhat.io/ubi8/ubi:latest",
					Origin:      "registry.redhat.io/ubi8/ubi:latest",
					Destination: "docker://test.registry.com/ubi8/ubi:v1.0",
					Type:        v2alpha1.TypeGeneric,
				},
			},
		},
		{
			name:        "mirrorToDisk with both TargetRepo and TargetTag",
			mode:        mirror.MirrorToDisk,
			destination: "oci://test",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:       "registry.redhat.io/ubi8/ubi:latest",
					TargetRepo: "custom-namespace/custom-image",
					TargetTag:  "v1.0",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://registry.redhat.io/ubi8/ubi:latest",
					Origin:      "registry.redhat.io/ubi8/ubi:latest",
					Destination: "docker://test.registry.com/custom-namespace/custom-image:v1.0",
					Type:        v2alpha1.TypeGeneric,
				},
			},
		},
		{
			name:        "mirrorToDisk with TargetTag for digest-only image",
			mode:        mirror.MirrorToDisk,
			destination: "oci://test",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:      "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
					TargetTag: "v1.0",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
					Origin:      "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
					Destination: "docker://test.registry.com/testns/test:v1.0",
					Type:        v2alpha1.TypeGeneric,
				},
			},
		},
		{
			name:        "mirrorToDisk with TargetRepo and TargetTag for OCI image",
			mode:        mirror.MirrorToDisk,
			destination: "oci://test",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:       "oci:///folder-a/folder-b/testns/test",
					TargetRepo: "custom/oci-image",
					TargetTag:  "v2.0",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "oci:///folder-a/folder-b/testns/test",
					Origin:      "oci:///folder-a/folder-b/testns/test",
					Destination: "docker://test.registry.com/custom/oci-image:v2.0",
					Type:        v2alpha1.TypeGeneric,
				},
			},
		},
		{
			name:        "invalid TargetRepo should skip image with warning and return error",
			mode:        mirror.MirrorToDisk,
			destination: "oci://test",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:       "registry.redhat.io/ubi8/ubi:latest",
					TargetRepo: "invalid:tag",
				},
				{
					Name: "registry.redhat.io/ubi9/ubi:latest",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://registry.redhat.io/ubi9/ubi:latest",
					Origin:      "registry.redhat.io/ubi9/ubi:latest",
					Destination: "docker://test.registry.com/ubi9/ubi:latest",
					Type:        v2alpha1.TypeGeneric,
				},
			},
			expectError: true,
			errContains: []string{"invalid targetRepo"},
		},
		{
			name:        "multiple invalid images should return joined errors",
			mode:        mirror.MirrorToDisk,
			destination: "oci://test",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name: "sometest.registry.com/testns/test@shainvaliddigest1",
				},
				{
					Name: "registry.redhat.io/ubi9/ubi:latest",
				},
				{
					Name:       "registry.redhat.io/ubi8/ubi:latest",
					TargetRepo: "invalid:tag",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://registry.redhat.io/ubi9/ubi:latest",
					Origin:      "registry.redhat.io/ubi9/ubi:latest",
					Destination: "docker://test.registry.com/ubi9/ubi:latest",
					Type:        v2alpha1.TypeGeneric,
				},
			},
			expectError: true,
			errContains: []string{
				"shainvaliddigest1",
				"invalid targetRepo",
			},
		},
		{
			name:        "diskToMirror with TargetRepo",
			mode:        mirror.DiskToMirror,
			destination: "docker://mirror.acme.com",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:       "registry.redhat.io/ubi8/ubi:latest",
					TargetRepo: "custom-namespace/custom-image",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://test.registry.com/custom-namespace/custom-image:latest",
					Origin:      "registry.redhat.io/ubi8/ubi:latest",
					Destination: "docker://mirror.acme.com/custom-namespace/custom-image:latest",
					Type:        v2alpha1.TypeGeneric,
				},
			},
		},
		{
			name:        "diskToMirror with TargetTag",
			mode:        mirror.DiskToMirror,
			destination: "docker://mirror.acme.com",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:      "registry.redhat.io/ubi8/ubi:latest",
					TargetTag: "v1.0",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://test.registry.com/ubi8/ubi:v1.0",
					Origin:      "registry.redhat.io/ubi8/ubi:latest",
					Destination: "docker://mirror.acme.com/ubi8/ubi:v1.0",
					Type:        v2alpha1.TypeGeneric,
				},
			},
		},
		{
			name:        "diskToMirror with TargetRepo and TargetTag",
			mode:        mirror.DiskToMirror,
			destination: "docker://mirror.acme.com",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:       "registry.redhat.io/ubi8/ubi:latest",
					TargetRepo: "custom-namespace/custom-image",
					TargetTag:  "v1.0",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://test.registry.com/custom-namespace/custom-image:v1.0",
					Origin:      "registry.redhat.io/ubi8/ubi:latest",
					Destination: "docker://mirror.acme.com/custom-namespace/custom-image:v1.0",
					Type:        v2alpha1.TypeGeneric,
				},
			},
		},
		{
			name:        "diskToMirror with TargetTag for digest-only image",
			mode:        mirror.DiskToMirror,
			destination: "docker://mirror.acme.com",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:      "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
					TargetTag: "v1.0",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://test.registry.com/testns/test:v1.0",
					Origin:      "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
					Destination: "docker://mirror.acme.com/testns/test:v1.0",
					Type:        v2alpha1.TypeGeneric,
				},
			},
		},
		{
			name:        "diskToMirror with TargetRepo and TargetTag for OCI image",
			mode:        mirror.DiskToMirror,
			destination: "docker://mirror.acme.com",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:       "oci:///folder-a/folder-b/testns/test",
					TargetRepo: "custom/oci-image",
					TargetTag:  "v2.0",
				},
			},
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://test.registry.com/custom/oci-image:v2.0",
					Origin:      "oci:///folder-a/folder-b/testns/test",
					Destination: "docker://mirror.acme.com/custom/oci-image:v2.0",
					Type:        v2alpha1.TypeGeneric,
				},
			},
		},
		{
			name:        "diskToMirror with generateV1Tags and TargetTag should use TargetTag",
			mode:        mirror.DiskToMirror,
			destination: "docker://mirror.acme.com",
			additionalImages: []v2alpha1.AdditionalImage{
				{
					Name:      "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
					TargetTag: "v1.0",
				},
			},
			useV1Tags: true,
			expected: []v2alpha1.CopyImageSchema{
				{
					Source:      "docker://test.registry.com/testns/test:v1.0",
					Origin:      "sometest.registry.com/testns/test@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea",
					Destination: "docker://mirror.acme.com/testns/test:v1.0",
					Type:        v2alpha1.TypeGeneric,
				},
			},
		},
	}

	log := clog.New("trace")
	global := &mirror.GlobalOptions{SecurePolicy: false}
	_, sharedOpts := mirror.SharedImageFlags()
	_, deprecatedTLSVerifyOpt := mirror.DeprecatedTLSVerifyFlags()
	_, srcOpts := mirror.ImageSrcFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "src-", "screds")
	_, destOpts := mirror.ImageDestFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "dest-", "dcreds")
	_, retryOpts := mirror.RetryFlags()
	localstorageFQDN := "test.registry.com"

	mockmirror := MockMirror{}
	manifest := MockManifest{Log: log}
	ctx := context.Background()

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := mirror.CopyOptions{
				Global:              global,
				DeprecatedTLSVerify: deprecatedTLSVerifyOpt,
				SrcImage:            srcOpts,
				DestImage:           destOpts,
				RetryOpts:           retryOpts,
				Destination:         c.destination,
				Dev:                 false,
				Mode:                c.mode,
				LocalStorageFQDN:    localstorageFQDN,
			}

			cfg := v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						AdditionalImages: c.additionalImages,
					},
				},
			}

			ex := New(log, cfg, opts, mockmirror, manifest)
			if c.useV1Tags {
				ex = WithV1Tags(ex)
			}

			res, err := ex.AdditionalImagesCollector(ctx)
			if c.expectError {
				require.Error(t, err)
				for _, substr := range c.errContains {
					assert.Contains(t, err.Error(), substr)
				}
			} else {
				require.NoError(t, err)
			}
			assert.ElementsMatch(t, c.expected, res.AllImages)
		})
	}
}

func TestAdditionalImageCollector_TagsByRegex(t *testing.T) {
	log := clog.New("trace")
	global := &mirror.GlobalOptions{SecurePolicy: false}
	_, sharedOpts := mirror.SharedImageFlags()
	_, deprecatedTLSVerifyOpt := mirror.DeprecatedTLSVerifyFlags()
	_, srcOpts := mirror.ImageSrcFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "src-", "screds")
	_, destOpts := mirror.ImageDestFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "dest-", "dcreds")
	_, retryOpts := mirror.RetryFlags()
	localstorageFQDN := "test.registry.com"
	ctx := context.Background()

	newOpts := func(workingDir, mode, destination string) mirror.CopyOptions {
		global := &mirror.GlobalOptions{SecurePolicy: false, WorkingDir: workingDir}
		return mirror.CopyOptions{
			Global:              global,
			DeprecatedTLSVerify: deprecatedTLSVerifyOpt,
			SrcImage:            srcOpts,
			DestImage:           destOpts,
			RetryOpts:           retryOpts,
			Destination:         destination,
			Mode:                mode,
			LocalStorageFQDN:    localstorageFQDN,
		}
	}

	t.Run("mirrorToDisk expands matching tags and writes cache", func(t *testing.T) {
		workingDir := t.TempDir()
		opts := newOpts(workingDir, mirror.MirrorToDisk, consts.OciProtocol+"test")
		var calls atomic.Int64
		manifest := MockManifest{
			Log:           log,
			RepoTags:      map[string][]string{"registry.example.com/team/tool": {"v1.0", "v1.1", "v2.0", "sha256-" + fmt.Sprintf("%064d", 0) + ".sig"}},
			RepoTagsCalls: &calls,
		}
		cfg := v2alpha1.ImageSetConfiguration{
			ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
				Mirror: v2alpha1.Mirror{
					AdditionalImages: []v2alpha1.AdditionalImage{
						{Name: "registry.example.com/team/tool", TagsByRegex: "^v1\\..*$"},
					},
				},
			},
		}
		ex := New(log, cfg, opts, MockMirror{}, manifest)
		res, err := ex.AdditionalImagesCollector(ctx)
		require.NoError(t, err)
		expected := []v2alpha1.CopyImageSchema{
			{
				Source:      consts.DockerProtocol + "registry.example.com/team/tool:v1.0",
				Origin:      "registry.example.com/team/tool:v1.0",
				Destination: consts.DockerProtocol + "test.registry.com/team/tool:v1.0",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Source:      consts.DockerProtocol + "registry.example.com/team/tool:v1.1",
				Origin:      "registry.example.com/team/tool:v1.1",
				Destination: consts.DockerProtocol + "test.registry.com/team/tool:v1.1",
				Type:        v2alpha1.TypeGeneric,
			},
		}
		assert.ElementsMatch(t, expected, res.AllImages)
		assert.Equal(t, int64(1), calls.Load())

		// metadata file should have been written under working-dir, nested
		// under a directory tree matching the repository name
		data, err := os.ReadFile(filepath.Join(workingDir, additionalImagesExtractDir, "registry.example.com/team/tool", "_meta.json"))
		require.NoError(t, err)
		expectedMeta, err := json.Marshal(repoMetadata{Tags: manifest.RepoTags["registry.example.com/team/tool"]})
		require.NoError(t, err)
		assert.JSONEq(t, string(expectedMeta), string(data))
	})

	t.Run("zero matches warns but does not error", func(t *testing.T) {
		workingDir := t.TempDir()
		opts := newOpts(workingDir, mirror.MirrorToDisk, consts.OciProtocol+"test")
		manifest := MockManifest{
			Log:      log,
			RepoTags: map[string][]string{"registry.example.com/team/tool": {"v2.0"}},
		}
		cfg := v2alpha1.ImageSetConfiguration{
			ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
				Mirror: v2alpha1.Mirror{
					AdditionalImages: []v2alpha1.AdditionalImage{
						{Name: "registry.example.com/team/tool", TagsByRegex: "^v1\\..*$"},
					},
				},
			},
		}
		ex := New(log, cfg, opts, MockMirror{}, manifest)
		res, err := ex.AdditionalImagesCollector(ctx)
		require.NoError(t, err)
		assert.Empty(t, res.AllImages)
	})

	t.Run("diskToMirror reuses the mirrorToDisk cache without calling GetRepositoryTags", func(t *testing.T) {
		workingDir := t.TempDir()
		m2dOpts := newOpts(workingDir, mirror.MirrorToDisk, consts.OciProtocol+"test")
		var seedCalls atomic.Int64
		seedManifest := MockManifest{
			Log:           log,
			RepoTags:      map[string][]string{"registry.example.com/team/tool": {"v1.0", "v1.1"}},
			RepoTagsCalls: &seedCalls,
		}
		cfg := v2alpha1.ImageSetConfiguration{
			ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
				Mirror: v2alpha1.Mirror{
					AdditionalImages: []v2alpha1.AdditionalImage{
						{Name: "registry.example.com/team/tool", TagsByRegex: "^v1\\..*$"},
					},
				},
			},
		}
		seedEx := New(log, cfg, m2dOpts, MockMirror{}, seedManifest)
		_, err := seedEx.AdditionalImagesCollector(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(1), seedCalls.Load())

		d2mOpts := newOpts(workingDir, mirror.DiskToMirror, consts.DockerProtocol+"mirror.acme.com")
		var d2mCalls atomic.Int64
		// no RepoTags configured: if GetRepositoryTags were called, the mock would error
		d2mManifest := MockManifest{Log: log, RepoTagsCalls: &d2mCalls}
		d2mEx := New(log, cfg, d2mOpts, MockMirror{}, d2mManifest)
		res, err := d2mEx.AdditionalImagesCollector(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), d2mCalls.Load())
		expected := []v2alpha1.CopyImageSchema{
			{
				Source:      consts.DockerProtocol + "test.registry.com/team/tool:v1.0",
				Origin:      "registry.example.com/team/tool:v1.0",
				Destination: consts.DockerProtocol + "mirror.acme.com/team/tool:v1.0",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Source:      consts.DockerProtocol + "test.registry.com/team/tool:v1.1",
				Origin:      "registry.example.com/team/tool:v1.1",
				Destination: consts.DockerProtocol + "mirror.acme.com/team/tool:v1.1",
				Type:        v2alpha1.TypeGeneric,
			},
		}
		assert.ElementsMatch(t, expected, res.AllImages)
	})

	t.Run("delete mode reuses the mirrorToDisk cache without calling GetRepositoryTags", func(t *testing.T) {
		workingDir := t.TempDir()
		m2dOpts := newOpts(workingDir, mirror.MirrorToDisk, consts.OciProtocol+"test")
		seedManifest := MockManifest{
			Log:      log,
			RepoTags: map[string][]string{"registry.example.com/team/tool": {"v1.0"}},
		}
		cfg := v2alpha1.ImageSetConfiguration{
			ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
				Mirror: v2alpha1.Mirror{
					AdditionalImages: []v2alpha1.AdditionalImage{
						{Name: "registry.example.com/team/tool", TagsByRegex: "^v1\\..*$"},
					},
				},
			},
		}
		seedEx := New(log, cfg, m2dOpts, MockMirror{}, seedManifest)
		_, err := seedEx.AdditionalImagesCollector(ctx)
		require.NoError(t, err)

		deleteOpts := newOpts(workingDir, mirror.DiskToMirror, consts.DockerProtocol+"mirror.acme.com")
		deleteOpts.Function = string(mirror.DeleteMode)
		var deleteCalls atomic.Int64
		deleteManifest := MockManifest{Log: log, RepoTagsCalls: &deleteCalls}
		deleteEx := New(log, cfg, deleteOpts, MockMirror{}, deleteManifest)
		res, err := deleteEx.AdditionalImagesCollector(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), deleteCalls.Load())
		require.Len(t, res.AllImages, 1)
	})

	t.Run("diskToMirror with no cache returns an error, not a network call", func(t *testing.T) {
		workingDir := t.TempDir()
		opts := newOpts(workingDir, mirror.DiskToMirror, consts.DockerProtocol+"mirror.acme.com")
		manifest := MockManifest{Log: log}
		cfg := v2alpha1.ImageSetConfiguration{
			ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
				Mirror: v2alpha1.Mirror{
					AdditionalImages: []v2alpha1.AdditionalImage{
						{Name: "registry.example.com/team/tool", TagsByRegex: "^v1\\..*$"},
					},
				},
			},
		}
		ex := New(log, cfg, opts, MockMirror{}, manifest)
		_, err := ex.AdditionalImagesCollector(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no cached tag list found")
	})

	t.Run("TargetRepo applies across multiple matched tags", func(t *testing.T) {
		workingDir := t.TempDir()
		opts := newOpts(workingDir, mirror.MirrorToDisk, consts.OciProtocol+"test")
		manifest := MockManifest{
			Log:      log,
			RepoTags: map[string][]string{"registry.example.com/team/tool": {"v1.0", "v1.1"}},
		}
		cfg := v2alpha1.ImageSetConfiguration{
			ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
				Mirror: v2alpha1.Mirror{
					AdditionalImages: []v2alpha1.AdditionalImage{
						{Name: "registry.example.com/team/tool", TagsByRegex: "^v1\\..*$", TargetRepo: "custom/tool"},
					},
				},
			},
		}
		ex := New(log, cfg, opts, MockMirror{}, manifest)
		res, err := ex.AdditionalImagesCollector(ctx)
		require.NoError(t, err)
		expected := []v2alpha1.CopyImageSchema{
			{
				Source:      consts.DockerProtocol + "registry.example.com/team/tool:v1.0",
				Origin:      "registry.example.com/team/tool:v1.0",
				Destination: consts.DockerProtocol + "test.registry.com/custom/tool:v1.0",
				Type:        v2alpha1.TypeGeneric,
			},
			{
				Source:      consts.DockerProtocol + "registry.example.com/team/tool:v1.1",
				Origin:      "registry.example.com/team/tool:v1.1",
				Destination: consts.DockerProtocol + "test.registry.com/custom/tool:v1.1",
				Type:        v2alpha1.TypeGeneric,
			},
		}
		assert.ElementsMatch(t, expected, res.AllImages)
	})

	t.Run("many TagsByRegex entries across distinct repos are expanded concurrently and in order", func(t *testing.T) {
		workingDir := t.TempDir()
		opts := newOpts(workingDir, mirror.MirrorToDisk, consts.OciProtocol+"test")

		const numRepos = 8
		repoTags := map[string][]string{}
		var additionalImages []v2alpha1.AdditionalImage
		var expectedOrigins []string
		for i := range numRepos {
			repo := fmt.Sprintf("registry.example.com/team/tool%d", i)
			repoTags[repo] = []string{"v1.0", "v2.0"}
			additionalImages = append(additionalImages, v2alpha1.AdditionalImage{Name: repo, TagsByRegex: "^v1\\..*$"})
			expectedOrigins = append(expectedOrigins, repo+":v1.0")
		}

		var calls atomic.Int64
		manifest := MockManifest{Log: log, RepoTags: repoTags, RepoTagsCalls: &calls}
		cfg := v2alpha1.ImageSetConfiguration{
			ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
				Mirror: v2alpha1.Mirror{AdditionalImages: additionalImages},
			},
		}
		ex := New(log, cfg, opts, MockMirror{}, manifest)
		res, err := ex.AdditionalImagesCollector(ctx)
		require.NoError(t, err)
		require.Len(t, res.AllImages, numRepos)
		assert.Equal(t, int64(numRepos), calls.Load())

		var gotOrigins []string
		for _, img := range res.AllImages {
			gotOrigins = append(gotOrigins, img.Origin)
		}
		assert.Equal(t, expectedOrigins, gotOrigins, "output order should match input order despite concurrent resolution")
	})

	t.Run("sig tags are always excluded even if the regex would match them", func(t *testing.T) {
		workingDir := t.TempDir()
		opts := newOpts(workingDir, mirror.MirrorToDisk, consts.OciProtocol+"test")
		sigTag := "sha256-" + fmt.Sprintf("%064d", 1) + ".sig"
		manifest := MockManifest{
			Log:      log,
			RepoTags: map[string][]string{"registry.example.com/team/tool": {"v1.0", sigTag}},
		}
		cfg := v2alpha1.ImageSetConfiguration{
			ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
				Mirror: v2alpha1.Mirror{
					// ".*" would otherwise match the .sig tag too
					AdditionalImages: []v2alpha1.AdditionalImage{
						{Name: "registry.example.com/team/tool", TagsByRegex: ".*"},
					},
				},
			},
		}
		ex := New(log, cfg, opts, MockMirror{}, manifest)
		res, err := ex.AdditionalImagesCollector(ctx)
		require.NoError(t, err)
		require.Len(t, res.AllImages, 1)
		assert.Equal(t, "registry.example.com/team/tool:v1.0", res.AllImages[0].Origin)
	})

	t.Run("invalid regex is reported as an error", func(t *testing.T) {
		workingDir := t.TempDir()
		opts := newOpts(workingDir, mirror.MirrorToDisk, consts.OciProtocol+"test")
		manifest := MockManifest{Log: log}
		cfg := v2alpha1.ImageSetConfiguration{
			ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
				Mirror: v2alpha1.Mirror{
					AdditionalImages: []v2alpha1.AdditionalImage{
						{Name: "registry.example.com/team/tool", TagsByRegex: "("},
					},
				},
			},
		}
		ex := New(log, cfg, opts, MockMirror{}, manifest)
		_, err := ex.AdditionalImagesCollector(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid tagsByRegex")
	})

	t.Run("tag or digest present in Name alongside TagsByRegex is an error", func(t *testing.T) {
		workingDir := t.TempDir()
		opts := newOpts(workingDir, mirror.MirrorToDisk, consts.OciProtocol+"test")
		manifest := MockManifest{Log: log}
		cfg := v2alpha1.ImageSetConfiguration{
			ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
				Mirror: v2alpha1.Mirror{
					AdditionalImages: []v2alpha1.AdditionalImage{
						{Name: "registry.example.com/team/tool:latest", TagsByRegex: ".*"},
					},
				},
			},
		}
		ex := New(log, cfg, opts, MockMirror{}, manifest)
		_, err := ex.AdditionalImagesCollector(ctx)
		require.Error(t, err)
	})
}

func (o MockMirror) Run(ctx context.Context, src, dest string, mode mirror.Mode, opts *mirror.CopyOptions) error {
	return nil
}

func (o MockMirror) Check(ctx context.Context, image string, opts *mirror.CopyOptions, asCopySrc bool) (bool, error) {
	return true, nil
}

func (o MockManifest) GetOperatorConfig(file string) (*v2alpha1.OperatorConfigSchema, error) {
	opcl := v2alpha1.OperatorLabels{OperatorsOperatorframeworkIoIndexConfigsV1: "/configs"}
	opc := v2alpha1.OperatorConfig{Labels: opcl}
	ocs := &v2alpha1.OperatorConfigSchema{Config: opc}
	return ocs, nil
}

func (o MockManifest) GetReleaseSchema(filePath string) ([]v2alpha1.RelatedImage, error) {
	relatedImages := []v2alpha1.RelatedImage{
		{Name: "testA", Image: "sometestimage-a@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea"},
		{Name: "testB", Image: "sometestimage-b@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea"},
		{Name: "testC", Image: "sometestimage-c@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea"},
		{Name: "testD", Image: "sometestimage-d@sha256:f30638f60452062aba36a26ee6c036feead2f03b28f2c47f2b0a991e41baebea"},
	}
	return relatedImages, nil
}

func (o MockManifest) GetOCIImageIndex(name string) (*specv1.Index, error) {
	d, err := digest.Parse("sha256:3ef0b0141abd1548f60c4f3b23ecfc415142b0e842215f38e98610a3b2e52419")
	if err != nil {
		return nil, fmt.Errorf("failed to parse digest: %w", err)
	}
	return &specv1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		Manifests: []specv1.Descriptor{
			{
				MediaType: specv1.MediaTypeImageManifest,
				Digest:    d,
				Size:      567,
			},
		},
	}, nil
}

func (o MockManifest) GetOCIImageManifest(name string) (*specv1.Manifest, error) {
	d, err := digest.Parse("sha256:3ef0b0141abd1548f60c4f3b23ecfc415142b0e842215f38e98610a3b2e52419")
	if err != nil {
		return nil, fmt.Errorf("failed to parse digest: %w", err)
	}
	return &specv1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		Config: specv1.Descriptor{
			MediaType: specv1.MediaTypeImageManifest,
			Digest:    d,
			Size:      567,
		},
	}, nil
}

func (o MockManifest) GetOCIImageFromIndex(dir string) (gcrv1.Image, error) { //nolint:ireturn // interface is expected here
	return nil, nil
}

func (o MockManifest) ExtractOCILayers(_ gcrv1.Image, toPath, label string) error {
	return nil
}

func (o MockManifest) ExtractLayers(filePath, name, label string) error {
	return nil
}

func (o MockManifest) ConvertOCIIndexToSingleManifest(dir string, oci *specv1.Index) error {
	return nil
}

func (o MockManifest) ImageDigest(ctx context.Context, sourceCtx *types.SystemContext, imgRef string) (string, error) {
	return "123456", nil
}

func (o MockManifest) ImageManifest(ctx context.Context, sourceCtx *types.SystemContext, imgRef string, instanceDigest *digest.Digest) ([]byte, string, error) {
	return nil, "", nil
}

func (o MockManifest) GetManifestListDigests(ctx context.Context, sourceCtx *types.SystemContext, source string) ([]string, error) {
	return nil, nil
}

func (o MockManifest) GetRepositoryTags(ctx context.Context, sourceCtx *types.SystemContext, imgRef string) ([]string, error) {
	if o.RepoTagsCalls != nil {
		o.RepoTagsCalls.Add(1)
	}
	tags, ok := o.RepoTags[imgRef]
	if !ok {
		return nil, fmt.Errorf("mock: no tags configured for repo %q", imgRef)
	}
	return tags, nil
}
