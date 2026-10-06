package baggageutil

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
)

func rawMember(t *testing.T, key, value string) baggage.Member {
	t.Helper()
	member, err := baggage.NewMemberRaw(key, value)
	require.NoError(t, err)
	return member
}

func TestBuildMemberLimitDeterministic(t *testing.T) {
	// Arrange: input order may come from an unordered SDK map.
	members := make([]baggage.Member, 0, MaxMembers+7)
	for index := range MaxMembers + 7 {
		members = append(members, rawMember(t, fmt.Sprintf("k%03d", index), "value"))
	}
	random := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		random.Shuffle(len(members), func(i, j int) { members[i], members[j] = members[j], members[i] })
		// Act.
		bag, dropped := Build(members)
		// Assert: the exact first 64 keys survive every input permutation.
		require.Equal(t, 7, dropped)
		require.Equal(t, MaxMembers, bag.Len())
		for index := range MaxMembers + 7 {
			if index < MaxMembers {
				require.Equal(t, "value", bag.Member(fmt.Sprintf("k%03d", index)).Value())
			} else {
				require.Empty(t, bag.Member(fmt.Sprintf("k%03d", index)).Value())
			}
		}
		require.LessOrEqual(t, len(bag.String()), MaxBytes)
	}
}

func TestBuildByteLimitAndPrefix(t *testing.T) {
	for _, test := range []struct {
		name    string
		members []baggage.Member
		kept    []string
		dropped int
	}{
		{name: "exact", members: []baggage.Member{rawMember(t, "a", strings.Repeat("x", MaxBytes-2))}, kept: []string{"a"}},
		{name: "over", members: []baggage.Member{rawMember(t, "a", strings.Repeat("x", MaxBytes-1))}, dropped: 1},
		{name: "comma", members: []baggage.Member{rawMember(t, "a", strings.Repeat("x", MaxBytes-4)), rawMember(t, "b", "x")}, kept: []string{"a"}, dropped: 1},
		{name: "prefix", members: []baggage.Member{rawMember(t, "c", "small"), rawMember(t, "a", "small"), rawMember(t, "b", strings.Repeat("x", MaxBytes))}, kept: []string{"a"}, dropped: 2},
		{name: "encoded", members: []baggage.Member{rawMember(t, "a", strings.Repeat("%", MaxBytes/3))}, kept: []string{"a"}},
		{name: "encoded over", members: []baggage.Member{rawMember(t, "a", strings.Repeat("%", MaxBytes/3+1))}, dropped: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange: use actual percent-encoded wire lengths, not raw text bytes.
			members := test.members
			// Act.
			bag, dropped := Build(members)
			// Assert.
			require.Equal(t, test.dropped, dropped)
			keys := make([]string, 0, bag.Len())
			for _, member := range bag.Members() {
				keys = append(keys, member.Key())
			}
			slices.Sort(keys)
			require.Len(t, keys, len(test.kept))
			for index, key := range keys {
				require.Equal(t, test.kept[index], key)
			}
			require.LessOrEqual(t, len(bag.String()), MaxBytes)
		})
	}
}

func TestBuildDuplicateLastWinsAndPinnedBounds(t *testing.T) {
	// Arrange.
	old := rawMember(t, "same", "old")
	latest := rawMember(t, "same", "new")
	// Act.
	bag, dropped := Build([]baggage.Member{old, latest})
	// Assert.
	require.Zero(t, dropped)
	require.Equal(t, "new", bag.Member("same").Value())
	// Confirm pinned SDK constraints that the implementation explicitly mirrors.
	_, err := baggage.New(rawMember(t, "a", strings.Repeat("x", MaxBytes-2)))
	require.NoError(t, err)
	_, err = baggage.New(rawMember(t, "a", strings.Repeat("x", MaxBytes-1)))
	require.Error(t, err)
	members := make([]baggage.Member, 0, MaxMembers+1)
	for index := range MaxMembers + 1 {
		members = append(members, rawMember(t, fmt.Sprintf("k%d", index), "x"))
	}
	_, err = baggage.New(members[:MaxMembers]...)
	require.NoError(t, err)
	_, err = baggage.New(members...)
	require.Error(t, err)
}
