package metry

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/internal/attrconv"
)

func TestAttributeToOTel_MatchesAttrconv(t *testing.T) {
	t.Parallel()

	cases := []Attribute{
		TenantID("t-1"),
		FloatAttribute("score", 0.91),
		BoolAttribute("passed", true),
		IntAttribute("retrieval_top_k", 5),
		StringAttribute("custom", "value"),
		{},
		newAttribute("invalid key /", "x"),
	}

	for _, attr := range cases {
		require.Equal(t, attr.toOTel(), attrconv.ToOTel(attr))
	}
}
