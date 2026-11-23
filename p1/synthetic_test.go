package p1

import (
	"strings"
	"testing"
)

func fixtureEntry(t *testing.T, entries []Entry, id string) Entry {
	t.Helper()
	var result Entry
	count := 0
	for _, entry := range entries {
		if entry.ID == id {
			result = entry
			count++
		}
	}
	if count != 1 {
		t.Fatalf("entry %q: got %d, want one", id, count)
	}
	return result
}

func fixtureHasCitation(entry Entry, ref Ref, paragraph string) bool {
	for _, citation := range entry.Citations {
		if citation.Ref == ref && citation.Paragraph == paragraph {
			return true
		}
	}
	return false
}

func fixtureCheckCitations(t *testing.T, entries []Entry, materials []Material) {
	t.Helper()
	for _, entry := range entries {
		if len(entry.Citations) == 0 {
			t.Fatalf("entry %s has no evidence", entry.ID)
		}
		for _, citation := range entry.Citations {
			found := false
			for _, material := range materials {
				if material.Ref != citation.Ref {
					continue
				}
				for _, paragraph := range material.Paragraphs {
					if paragraph.ID == citation.Paragraph && paragraph.Text != "" {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("entry %s cites absent %s %s", entry.ID, citation.Ref.String(), citation.Paragraph)
			}
		}
	}
}

func TestSyntheticFixturesAreExactAndIndependent(t *testing.T) {
	materials := syntheticMaterials()
	if len(materials) != 3 {
		t.Fatalf("got %d materials, want only three", len(materials))
	}
	for i, want := range []string{"S-A@v1", "S-B@v1", "S-C@v1"} {
		if materials[i].Ref.String() != want {
			t.Fatalf("material %d: got %s, want %s", i, materials[i].Ref.String(), want)
		}
	}
	materials[0].Paragraphs[0].Text = "changed"
	if syntheticMaterials()[0].Paragraphs[0].Text == "changed" {
		t.Fatal("fixture calls share paragraph storage")
	}
}

func TestSyntheticConflictIsSingleAndScopesStaySeparate(t *testing.T) {
	materials := syntheticMaterials()[:2]
	entries := buildEntries(materials)
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want four distinct topics", len(entries))
	}
	alpha := fixtureEntry(t, entries, "alpha-retention")
	if alpha.Category != "冲突" || !strings.Contains(alpha.Description, "90 天") || !strings.Contains(alpha.Description, "至少保存 180 天") {
		t.Fatalf("newer statement must remain one conflict: %+v", alpha)
	}
	for _, want := range []Citation{
		{Ref: Ref{ID: "S-A", Version: "v1"}, Paragraph: "P01"},
		{Ref: Ref{ID: "S-A", Version: "v1"}, Paragraph: "P02"},
		{Ref: Ref{ID: "S-B", Version: "v1"}, Paragraph: "P01"},
	} {
		if !fixtureHasCitation(alpha, want.Ref, want.Paragraph) {
			t.Fatalf("conflict lacks %s %s", want.Ref.String(), want.Paragraph)
		}
	}
	beta := fixtureEntry(t, entries, "beta-retention")
	if beta.Category != "明确" || beta.Scope != "乙产品维护期" || !strings.Contains(beta.Description, "30 天") {
		t.Fatalf("different scope changed by alpha conflict: %+v", beta)
	}
	quote := fixtureEntry(t, entries, "quote")
	if quote.Category != "待补问" || !strings.Contains(quote.Description, "一万元") {
		t.Fatalf("price suggestion gained confirmation: %+v", quote)
	}
	acceptance := fixtureEntry(t, entries, "acceptance")
	if acceptance.Category != "待补问" || len(acceptance.Citations) != 1 || acceptance.Citations[0].Ref.ID != "S-B" || acceptance.Citations[0].Paragraph != "P03" {
		t.Fatalf("attachment contents were invented: %+v", acceptance)
	}
	fixtureCheckCitations(t, entries, materials)
}

func TestSyntheticFormalReplacementRetainsOriginalAndAuthority(t *testing.T) {
	materials := syntheticMaterials()
	before := buildEntries(materials[:2])
	after := buildEntries(materials)
	alpha := fixtureEntry(t, after, "alpha-retention")
	if alpha.Category != "明确" || !strings.Contains(alpha.Description, "保存 180 天") {
		t.Fatalf("formal replacement not recognized: %+v", alpha)
	}
	for _, want := range []Citation{
		{Ref: Ref{ID: "S-A", Version: "v1"}, Paragraph: "P01"},
		{Ref: Ref{ID: "S-A", Version: "v1"}, Paragraph: "P02"},
		{Ref: Ref{ID: "S-C", Version: "v1"}, Paragraph: "P01"},
	} {
		if !fixtureHasCitation(alpha, want.Ref, want.Paragraph) {
			t.Fatalf("replacement lacks exact authority/original/new citation %+v", want)
		}
	}
	if fixtureEntry(t, before, "alpha-retention").Category != "冲突" {
		t.Fatal("later classification rewrote earlier result")
	}
	if beta := fixtureEntry(t, after, "beta-retention"); beta.Category != "明确" || !strings.Contains(beta.Description, "30 天") {
		t.Fatalf("replacement changed unrelated scope: %+v", beta)
	}
	fixtureCheckCitations(t, after, materials)
}

func TestSyntheticMissingMaterialsDoNotInventEvidence(t *testing.T) {
	all := syntheticMaterials()
	onlyA := buildEntries(all[:1])
	if alpha := fixtureEntry(t, onlyA, "alpha-retention"); alpha.Category != "明确" || !strings.Contains(alpha.Description, "90 天") {
		t.Fatalf("available original requirement lost: %+v", alpha)
	}
	for _, entry := range onlyA {
		if entry.ID == "acceptance" {
			t.Fatal("generated acceptance topic without its paragraph")
		}
	}
	onlyB := buildEntries(all[1:2])
	for _, entry := range onlyB {
		if entry.Category != "待补问" || strings.Contains(entry.Description, "90 天") || entry.Scope == "乙产品维护期" {
			t.Fatalf("missing A supplied facts or authority: %+v", entry)
		}
	}
	fixtureCheckCitations(t, onlyB, all[1:2])
	if got := buildEntries(nil); len(got) != 0 {
		t.Fatalf("empty input generated %d entries", len(got))
	}
	if got := buildEntries([]Material{{Ref: all[0].Ref}}); len(got) != 0 {
		t.Fatal("material identity alone generated entries")
	}
}

func TestSyntheticReplacementWithoutAuthorityRemainsUnresolved(t *testing.T) {
	materials := syntheticMaterials()
	materials[0].Paragraphs = materials[0].Paragraphs[1:]
	entries := buildEntries(materials)
	if alpha := fixtureEntry(t, entries, "alpha-retention"); alpha.Category != "冲突" {
		t.Fatalf("replacement without authority resolved conflict: %+v", alpha)
	}
	if beta := fixtureEntry(t, entries, "beta-retention"); beta.Category != "待补问" {
		t.Fatalf("missing authority was silently recovered: %+v", beta)
	}
	fixtureCheckCitations(t, entries, materials)
}

func TestSyntheticChangedContentAndVersionsCannotBorrowFixtureIdentity(t *testing.T) {
	for _, field := range []string{"text", "speaker", "date", "version"} {
		t.Run(field, func(t *testing.T) {
			material := syntheticMaterials()[2]
			switch field {
			case "text":
				material.Paragraphs[0].Text = "甲产品试点录音保存 999 天。"
			case "speaker":
				material.Paragraphs[0].Speaker = "陌生人"
			case "date":
				material.Paragraphs[0].Date = "2099-01-01"
			case "version":
				material.Ref.Version = "v2"
			}
			if entries := buildEntries([]Material{material}); len(entries) != 0 {
				t.Fatalf("modified %s borrowed fixture identity: %+v", field, entries)
			}
		})
	}
	materials := syntheticMaterials()
	materials[2].Ref.Version = "v2"
	if alpha := fixtureEntry(t, buildEntries(materials), "alpha-retention"); alpha.Category != "冲突" {
		t.Fatalf("unrecognized replacement version resolved conflict: %+v", alpha)
	}
}

func TestSyntheticDuplicateEvidenceDoesNotDuplicateDisagreement(t *testing.T) {
	materials := syntheticMaterials()[:2]
	materials = append(materials, materials[0], materials[1])
	entries := buildEntries(materials)
	alpha := fixtureEntry(t, entries, "alpha-retention")
	if len(entries) != 4 || alpha.Category != "冲突" || len(alpha.Citations) != 3 {
		t.Fatalf("repeated evidence duplicated disagreement: %+v", entries)
	}
	changed := syntheticMaterials()[0]
	changed.Paragraphs[1].Text = "甲产品试点录音保存 999 天。"
	materials = append(materials, changed)
	entries = buildEntries(materials)
	for _, entry := range entries {
		if fixtureHasCitation(entry, Ref{ID: "S-A", Version: "v1"}, "P02") {
			t.Fatalf("inconsistent paragraph identity remained citable: %+v", entry)
		}
	}
}
