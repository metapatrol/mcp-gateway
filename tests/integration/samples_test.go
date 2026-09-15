package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The samples are documentation people copy. Each mcp-gateway resource in them is
// sent to the API server as a dry run, so a sample the schema refuses — or one
// with a field the schema would silently drop — fails here rather than for
// whoever applies it.
func TestSamplesAreAccepted(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "samples", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, file := range files {
		raw, err := os.ReadFile(file)
		require.NoError(t, err)
		decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
		for {
			var obj unstructured.Unstructured
			if err := decoder.Decode(&obj.Object); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err, file)
			}
			if obj.Object == nil || !strings.HasPrefix(obj.GetAPIVersion(), "mcp-gateway.metapatrol.com/") {
				continue
			}
			err := k8sClient.Create(context.Background(), &obj, client.DryRunAll, client.FieldValidation("Strict"))
			require.NoError(t, err, "%s: %s %s", filepath.Base(file), obj.GetKind(), obj.GetName())
		}
	}
}
