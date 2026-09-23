package config

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openshift/oc-mirror/v2/internal/pkg/api/v2alpha1"
)

func TestValidate(t *testing.T) {
	type spec struct {
		name     string
		config   *v2alpha1.ImageSetConfiguration
		expError string
	}

	cases := []spec{
		{
			name: "Valid/UniqueCatalogs",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog: "test-catalog1:latest",
							},
							{
								Catalog: "test-catalog2:latest",
							},
						},
					},
				},
			},
		},
		{
			name: "Valid/UniqueCatalogsWithTarget",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog:       "test-catalog:latest",
								TargetCatalog: "test1",
							},
							{
								Catalog:       "test-catalog:latest",
								TargetCatalog: "test2",
							},
						},
					},
				},
			},
		},
		{
			name: "Valid/UniqueCatalogsWithTargetCatalogAndTargetTag",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog:       "test-catalog:latest",
								TargetCatalog: "test1",
								TargetTag:     "v1.3",
							},
							{
								Catalog:       "test-catalog:latest",
								TargetCatalog: "test2",
								TargetTag:     "latest",
							},
						},
					},
				},
			},
		},
		{
			name: "Valid/UniqueReleaseChannels",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Platform: v2alpha1.Platform{
							Architectures: []string{v2alpha1.DefaultPlatformArchitecture},
							Channels: []v2alpha1.ReleaseChannel{
								{
									Name: "channel1",
								},
								{
									Name: "channel2",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Invalid/DuplicateCatalogs",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog: "test-catalog:latest",
							},
							{
								Catalog: "test-catalog:latest",
							},
						},
					},
				},
			},
			expError: "invalid configuration: catalog \"test-catalog:latest\": duplicate found in configuration",
		},
		{
			name: "Invalid/DuplicateCatalogsWithTarget",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog:       "test-catalog1:latest",
								TargetCatalog: "test",
							},
							{
								Catalog:       "test-catalog2:latest",
								TargetCatalog: "test",
							},
						},
					},
				},
			},
			expError: "invalid configuration: catalog \"test:latest\": duplicate found in configuration",
		},
		{
			name: "Invalid/CatalogWithTargetCatalogContainsTag",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog:       "test-catalog1:latest",
								TargetCatalog: "test:v1.3",
							},
						},
					},
				},
			},
			expError: "invalid configuration: invalid target path component \"test:v1.3\": should not contain a tag or digest. Expected format is 1 or more path components separated by /, where each path component is a set of alpha-numeric and regexp (?:[._]|__|[-]*). For more, see https://github.com/containers/image/blob/main/docker/reference/regexp.go",
		},
		{
			name: "Invalid/CatalogWithTargetCatalogContainsDigest",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog:       "test-catalog1:latest",
								TargetCatalog: "a/b/test@sha256:45df874",
							},
						},
					},
				},
			},
			expError: "invalid configuration: invalid target path component \"a/b/test@sha256:45df874\": should not contain a tag or digest. Expected format is 1 or more path components separated by /, where each path component is a set of alpha-numeric and regexp (?:[._]|__|[-]*). For more, see https://github.com/containers/image/blob/main/docker/reference/regexp.go",
		},
		{
			name: "Invalid/CatalogFilteringIncorrectChannelVersions",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog: "test-catalog1:latest",
								IncludeConfig: v2alpha1.IncludeConfig{
									Packages: []v2alpha1.IncludePackage{
										{
											Name: "operator1",
											Channels: []v2alpha1.IncludeChannel{
												{
													Name: "fast",
													IncludeBundle: v2alpha1.IncludeBundle{
														MaxVersion: "abc",
														MinVersion: "-+?",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expError: "invalid configuration: [catalog \"test-catalog1:latest\": operator \"operator1\": channel \"fast\": maxVersion \"abc\" must respect semantic versioning notation, catalog \"test-catalog1:latest\": operator \"operator1\": channel \"fast\": minVersion \"-+?\" must respect semantic versioning notation]",
		},
		{
			name: "Invalid/CatalogFilteringIncorrectVersions",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog: "test-catalog1:latest",
								IncludeConfig: v2alpha1.IncludeConfig{
									Packages: []v2alpha1.IncludePackage{
										{
											Name: "operator1",
											IncludeBundle: v2alpha1.IncludeBundle{
												MaxVersion: "abc",
												MinVersion: "-+?",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expError: "invalid configuration: [catalog \"test-catalog1:latest\": operator \"operator1\": maxVersion \"abc\" must respect semantic versioning notation, catalog \"test-catalog1:latest\": operator \"operator1\": minVersion \"-+?\" must respect semantic versioning notation]",
		},
		{
			name: "Invalid/CatalogFilteringByMinVersionAndChannelMaxVersion",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog: "test-catalog1:latest",
								IncludeConfig: v2alpha1.IncludeConfig{
									Packages: []v2alpha1.IncludePackage{
										{
											Name: "operator1",
											IncludeBundle: v2alpha1.IncludeBundle{
												MinVersion: "1.2.3",
											},
											Channels: []v2alpha1.IncludeChannel{
												{
													Name: "stable",
													IncludeBundle: v2alpha1.IncludeBundle{
														MaxVersion: "3.2.1",
														MinVersion: "1.1.1",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expError: "invalid configuration: catalog \"test-catalog1:latest\": operator \"operator1\": mixing both filtering by minVersion/maxVersion and filtering by channel minVersion/maxVersion is not allowed",
		},
		{
			name: "Invalid/DuplicateChannels",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Platform: v2alpha1.Platform{
							Channels: []v2alpha1.ReleaseChannel{
								{
									Name: "channel",
								},
								{
									Name: "channel",
								},
							},
						},
					},
				},
			},
			expError: "invalid configuration: release channel \"channel\": duplicate found in configuration",
		},
		{
			name: "Invalid/DuplicateOperatorPackages",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog: "test-catalog1:latest",
								IncludeConfig: v2alpha1.IncludeConfig{
									Packages: []v2alpha1.IncludePackage{
										{
											Name:           "package1",
											DefaultChannel: "stable",
										},
										{
											Name:           "package1",
											DefaultChannel: "fast",
										},
										{
											Name: "package2",
										},
									},
								},
							},
						},
					},
				},
			},
			expError: `invalid configuration: catalog "test-catalog1:latest": duplicate package entry "package1"`,
		},
		{
			name: "Valid/BlockedImagesWithValidRegex",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						BlockedImages: []v2alpha1.BlockedImage{
							{Name: "registry/namespace/image@sha256:.*"},
							{Name: "registry\\.example\\.com/.*"},
						},
					},
				},
			},
		},
		{
			name: "Invalid/BlockedImageWithMalformedRegex",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						BlockedImages: []v2alpha1.BlockedImage{
							{Name: "[invalid"},
						},
					},
				},
			},
			expError: `invalid configuration: blocked image "[invalid": invalid regular expression: error parsing regexp: missing closing ]: ` + "`[invalid`",
		},
		{
			name: "Invalid/DuplicateOperatorPackageChannels",
			config: &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{
						Operators: []v2alpha1.Operator{
							{
								Catalog: "test-catalog1:latest",
								IncludeConfig: v2alpha1.IncludeConfig{
									Packages: []v2alpha1.IncludePackage{
										{
											Name:           "package1",
											DefaultChannel: "stable",
											Channels: []v2alpha1.IncludeChannel{
												{
													Name: "channel1",
													IncludeBundle: v2alpha1.IncludeBundle{
														MinVersion: "1.1.1",
													},
												},
												{
													Name: "channel1",
													IncludeBundle: v2alpha1.IncludeBundle{
														MaxVersion: "2.2.2",
													},
												},
												{
													Name: "channel2",
													IncludeBundle: v2alpha1.IncludeBundle{
														MinVersion: "1.1.1",
														MaxVersion: "2.2.2",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expError: `invalid configuration: catalog "test-catalog1:latest": operator "package1": duplicate channel entry "channel1"`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.config)
			if c.expError != "" {
				require.EqualError(t, err, c.expError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateAdditionalImagesTagsByRegex(t *testing.T) {
	type spec struct {
		name        string
		images      []v2alpha1.AdditionalImage
		expErrorHas []string
	}

	_, regexErr := regexp.Compile("(")
	require.Error(t, regexErr)

	cases := []spec{
		{
			name: "valid tagsByRegex with bare repo name",
			images: []v2alpha1.AdditionalImage{
				{Name: "registry.example.com/team/tool", TagsByRegex: "^v1\\..*$"},
			},
		},
		{
			name: "invalid regex",
			images: []v2alpha1.AdditionalImage{
				{Name: "registry.example.com/team/tool", TagsByRegex: "("},
			},
			expErrorHas: []string{`invalid tagsByRegex "(": ` + regexErr.Error()},
		},
		{
			name: "targetTag conflicts with tagsByRegex",
			images: []v2alpha1.AdditionalImage{
				{Name: "registry.example.com/team/tool", TagsByRegex: ".*", TargetTag: "v1.0"},
			},
			expErrorHas: []string{"targetTag is not allowed when tagsByRegex is set"},
		},
		{
			name: "tagged name conflicts with tagsByRegex",
			images: []v2alpha1.AdditionalImage{
				{Name: "registry.example.com/team/tool:latest", TagsByRegex: ".*"},
			},
			expErrorHas: []string{"name must be a bare repository (no tag or digest) when tagsByRegex is set"},
		},
		{
			name: "digested name conflicts with tagsByRegex",
			images: []v2alpha1.AdditionalImage{
				{Name: "registry.example.com/team/tool@sha256:44d75007b39e0e1bbf1bcfd0721245add54c54c3f83903f8926fb4bef6827aa2", TagsByRegex: ".*"},
			},
			expErrorHas: []string{"name must be a bare repository (no tag or digest) when tagsByRegex is set"},
		},
		{
			name: "entries without tagsByRegex are untouched",
			images: []v2alpha1.AdditionalImage{
				{Name: "registry.example.com/team/tool:latest", TargetTag: "v1.0"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &v2alpha1.ImageSetConfiguration{
				ImageSetConfigurationSpec: v2alpha1.ImageSetConfigurationSpec{
					Mirror: v2alpha1.Mirror{AdditionalImages: c.images},
				},
			}
			err := Validate(cfg)
			if len(c.expErrorHas) == 0 {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, substr := range c.expErrorHas {
				assert.Contains(t, err.Error(), substr)
			}
		})
	}

	t.Run("delete config validation covers the same rules", func(t *testing.T) {
		cfg := &v2alpha1.DeleteImageSetConfiguration{
			DeleteImageSetConfigurationSpec: v2alpha1.DeleteImageSetConfigurationSpec{
				Delete: v2alpha1.Delete{
					AdditionalImages: []v2alpha1.AdditionalImage{
						{Name: "registry.example.com/team/tool", TagsByRegex: "(", TargetTag: "v1.0"},
					},
				},
			},
		}
		err := ValidateDelete(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid tagsByRegex")
		assert.Contains(t, err.Error(), "targetTag is not allowed")
	})
}
