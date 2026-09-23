package cli

import (
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openshift/oc-mirror/v2/internal/pkg/consts"
	clog "github.com/openshift/oc-mirror/v2/internal/pkg/log"
	"github.com/openshift/oc-mirror/v2/internal/testutils"
)

// TestEnvironmentAdditionalTagsByRegex exercises additionalImages tagsByRegex
// end-to-end: mirrorToDisk caches the matching tags from a fake source registry,
// and diskToMirror replays that cache without touching the source.
type TestEnvironmentAdditionalTagsByRegex struct {
	sourceServer              *httptest.Server
	destinationServer         *httptest.Server
	sourceRegistryDomain      string
	destinationRegistryDomain string
	tempFolder                string
	imageSetConfig            string
	repo                      string
	matchingTags              []string
	nonMatchingTags           []string
}

func setupAdditionalTagsByRegexTest(t *testing.T) TestEnvironmentAdditionalTagsByRegex {
	suite := TestEnvironmentAdditionalTagsByRegex{}

	suite.sourceServer = testutils.CreateRegistry()
	us, err := url.Parse(suite.sourceServer.URL)
	require.NoError(t, err, "should not fail to get url of source registry")
	suite.sourceRegistryDomain = us.Host

	suite.destinationServer = testutils.CreateRegistry()
	ud, err := url.Parse(suite.destinationServer.URL)
	require.NoError(t, err, "should not fail to get url of destination registry")
	suite.destinationRegistryDomain = ud.Host

	suite.tempFolder = t.TempDir()
	suite.matchingTags = []string{"alpha", "beta"}
	suite.nonMatchingTags = []string{"gamma"}
	suite.repo = suite.sourceRegistryDomain + "/tagsbyregex"

	for _, tag := range append(append([]string{}, suite.matchingTags...), suite.nonMatchingTags...) {
		_, err := testutils.GenerateFakeImage("tagsbyregex", suite.repo+":"+tag, suite.tempFolder)
		require.NoError(t, err, "should not fail to push image %s:%s", suite.repo, tag)
	}

	templatePath := "../../e2e/templates/isc_templates/additional_tagsbyregex_isc.yaml"
	suite.imageSetConfig = suite.tempFolder + "/isc.yaml"
	err = testutils.FileFromTemplate(suite.imageSetConfig, templatePath, []string{suite.repo, "^(alpha|beta)$"})
	require.NoError(t, err, "should not fail to generate imageSetConfig")

	return suite
}

func (suite *TestEnvironmentAdditionalTagsByRegex) tearDownSource() {
	suite.sourceServer.Close()
}

func (suite *TestEnvironmentAdditionalTagsByRegex) tearDown() {
	suite.tearDownSource()
	suite.destinationServer.Close()
	os.RemoveAll(suite.tempFolder)
}

func (suite *TestEnvironmentAdditionalTagsByRegex) copyArchiveForD2M(t *testing.T) {
	d2mPath := filepath.Join(suite.tempFolder, "run-tagsbyregex", d2mSubFolder)
	err := os.MkdirAll(d2mPath, 0755)
	require.NoError(t, err, "should not fail creating "+d2mPath)
	archivePath := filepath.Join(suite.tempFolder, "run-tagsbyregex", m2dSubFolder, "mirror_000001.tar")

	srcArchive, err := os.Open(archivePath)
	require.NoError(t, err, "should not fail opening archive after Mirror2Disk")
	defer srcArchive.Close()

	destArchive, err := os.Create(filepath.Join(d2mPath, "mirror_000001.tar"))
	require.NoError(t, err, "should not fail creating archive file under "+d2mPath)
	defer destArchive.Close()

	_, err = io.Copy(destArchive, srcArchive)
	require.NoError(t, err, "should not fail copying archive file under "+d2mPath)
}

// TestIntegrationAdditionalTagsByRegex runs mirrorToDisk (against the real
// fake source registry) then diskToMirror (with the source registry shut
// down first) to prove diskToMirror never re-queries the source for the
// tag list - it must replay the cache written during mirrorToDisk.
func TestIntegrationAdditionalTagsByRegex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	suite := setupAdditionalTagsByRegexTest(t)
	defer suite.tearDown()

	suite.runMirror2Disk(t)
	suite.copyArchiveForD2M(t)

	// prove diskToMirror does not need the source registry: only the
	// cached tag list (written during mirrorToDisk) and the local disk
	// cache (shipped in the archive) may be used.
	suite.tearDownSource()

	suite.runDisk2Mirror(t)
}

func (suite *TestEnvironmentAdditionalTagsByRegex) runMirror2Disk(t *testing.T) {
	ocmirror := NewMirrorCmd(clog.New("trace"))
	resultFolder := filepath.Join(suite.tempFolder, "run-tagsbyregex", m2dSubFolder)
	err := os.MkdirAll(resultFolder, 0755)
	require.NoError(t, err, "should not fail creating a temp folder for results")

	t.Setenv("OC_MIRROR_CACHE", suite.tempFolder+"/.cacheM2D")
	ocmirror.SetArgs([]string{"-c", suite.imageSetConfig, "--v2", "-p", "55011", "--src-tls-verify=false", "--dest-tls-verify=false", consts.FileProtocol + resultFolder})
	err = ocmirror.Execute()
	require.NoError(t, err, "should not fail executing oc-mirror mirrorToDisk")

	assert.FileExists(t, filepath.Join(resultFolder, "mirror_000001.tar"))
}

func (suite *TestEnvironmentAdditionalTagsByRegex) runDisk2Mirror(t *testing.T) {
	ocmirror := NewMirrorCmd(clog.New("trace"))
	resultFolder := filepath.Join(suite.tempFolder, "run-tagsbyregex", d2mSubFolder)
	t.Setenv("OC_MIRROR_CACHE", suite.tempFolder+"/.cacheD2M")
	ocmirror.SetArgs([]string{"-c", suite.imageSetConfig, "--v2", "-p", "55012", "--from", consts.FileProtocol + resultFolder, "--src-tls-verify=false", "--dest-tls-verify=false", consts.DockerProtocol + suite.destinationRegistryDomain + "/tagsbyregex"})
	err := ocmirror.Execute()
	require.NoError(t, err, "should not fail executing oc-mirror diskToMirror")

	for _, tag := range suite.matchingTags {
		destImgRef := strings.Replace(suite.repo, suite.sourceRegistryDomain, suite.destinationRegistryDomain+"/tagsbyregex", 1) + ":" + tag
		exists, err := testutils.ImageExists(destImgRef)
		assert.NoError(t, err, "should not fail checking existence of %s", destImgRef)
		assert.True(t, exists, "expected %s to have been mirrored", destImgRef)
	}

	for _, tag := range suite.nonMatchingTags {
		destImgRef := strings.Replace(suite.repo, suite.sourceRegistryDomain, suite.destinationRegistryDomain+"/tagsbyregex", 1) + ":" + tag
		exists, _ := testutils.ImageExists(destImgRef)
		assert.False(t, exists, "did not expect %s to have been mirrored", destImgRef)
	}
}
