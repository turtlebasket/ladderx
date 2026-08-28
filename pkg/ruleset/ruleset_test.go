package ruleset

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	validYAML = `
- domain: example.com
  regexRules:
    - match: "^http:"
      replace: "https:"`

	invalidYAML = `
- domain: [thisIsATestYamlThatIsMeantToFail.example]
  regexRules:
    - match: "^http:"
      replace: "https:"
    - match: "[incomplete"`
)

func TestLoadRulesFromRemoteFile(t *testing.T) {
	sourceRules, err := loadRuleFromString(t, validYAML)
	require.NoError(t, err)

	gzipReader, err := sourceRules.GzipYaml()
	require.NoError(t, err)
	gzipBody, err := io.ReadAll(gzipReader)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var writeErr error
		switch r.URL.Path {
		case "/valid-config.yml":
			_, writeErr = io.WriteString(w, validYAML)
		case "/invalid-config.yml":
			_, writeErr = io.WriteString(w, invalidYAML)
		case "/valid-config.gz":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, writeErr = w.Write(gzipBody)
		default:
			http.NotFound(w, r)
		}
		if writeErr != nil {
			t.Errorf("write fixture response: %v", writeErr)
		}
	}))
	t.Cleanup(server.Close)

	for _, path := range []string{"/valid-config.yml", "/valid-config.gz"} {
		t.Run(path, func(t *testing.T) {
			rs, err := NewRuleset(server.URL + path)
			require.NoError(t, err)
			require.Len(t, rs, 1)
			assert.Equal(t, "example.com", rs[0].Domain)
		})
	}

	_, err = NewRuleset(server.URL + "/invalid-config.yml")
	require.Error(t, err)

	t.Setenv("RULESET", server.URL+"/valid-config.gz")
	rs := NewRulesetFromEnv()
	require.Len(t, rs, 1)
	assert.Equal(t, "example.com", rs[0].Domain)
}

func loadRuleFromString(t *testing.T, yaml string) (RuleSet, error) {
	t.Helper()

	rulesPath := filepath.Join(t.TempDir(), "ruleset.yaml")
	require.NoError(t, os.WriteFile(rulesPath, []byte(yaml), 0o600))

	rs := RuleSet{}
	err := rs.loadRulesFromLocalFile(rulesPath)

	return rs, err
}

// TestLoadRulesFromLocalFile tests the loading of rules from a local YAML file.
func TestLoadRulesFromLocalFile(t *testing.T) {
	rs, err := loadRuleFromString(t, validYAML)
	require.NoError(t, err)
	require.Len(t, rs, 1)

	assert.Equal(t, "example.com", rs[0].Domain)
	require.Len(t, rs[0].RegexRules, 1)
	assert.Equal(t, "^http:", rs[0].RegexRules[0].Match)
	assert.Equal(t, "https:", rs[0].RegexRules[0].Replace)

	_, err = loadRuleFromString(t, invalidYAML)
	require.Error(t, err)
}

// TestLoadRulesFromLocalDir tests the loading of rules from a local nested directory full of yaml rulesets
func TestLoadRulesFromLocalDir(t *testing.T) {
	baseDir := t.TempDir()
	nestedDir := filepath.Join(baseDir, "nested")
	nestedTwiceDir := filepath.Join(nestedDir, "nestedTwice")
	require.NoError(t, os.MkdirAll(nestedTwiceDir, 0o755))

	testCases := []string{"test.yaml", "test2.yaml", "test-3.yaml", "test 4.yaml", "1987.test.yaml.yml", "foobar.example.com.yaml", "foobar.com.yml"}
	for _, fileName := range testCases {
		paths := []string{
			filepath.Join(nestedDir, "2x-"+fileName),
			filepath.Join(nestedTwiceDir, fileName),
			filepath.Join(baseDir, "base-"+fileName),
		}
		for _, path := range paths {
			require.NoError(t, os.WriteFile(path, []byte(validYAML), 0o600))
		}
	}

	rs := RuleSet{}
	err := rs.loadRulesFromLocalDir(baseDir)

	require.NoError(t, err)
	assert.Equal(t, len(testCases)*3, rs.Count())

	for _, rule := range rs {
		assert.Equal(t, "example.com", rule.Domain)
		require.Len(t, rule.RegexRules, 1)
		assert.Equal(t, "^http:", rule.RegexRules[0].Match)
		assert.Equal(t, "https:", rule.RegexRules[0].Replace)
	}
}
