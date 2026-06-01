package htmledit

import (
	"reflect"
	"strings"
	"testing"
	"testing/quick"

	"github.com/PuerkitoBio/goquery"
)

func testDocument(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	return doc
}

func TestNthOutOfRangeDoesNotAffectLastMatch(t *testing.T) {
	doc := testDocument(t, `<html><body><p id="first">one</p><p id="second">two</p></body></html>`)

	results, totalAffected, opsApplied, allReadOnly := ApplyOperations(doc, []Operation{
		{Type: "delete", Selector: "p", Nth: 3},
	})
	if allReadOnly {
		t.Fatal("delete operation should not be read-only")
	}
	if totalAffected != 0 || opsApplied != 0 {
		t.Fatalf("out-of-range nth affected elements: total=%d applied=%d results=%+v", totalAffected, opsApplied, results)
	}

	rendered, err := RenderDocument(doc, false)
	if err != nil {
		t.Fatalf("render document: %v", err)
	}
	for _, id := range []string{`id="first"`, `id="second"`} {
		if !strings.Contains(rendered, id) {
			t.Fatalf("out-of-range nth removed %s from %s", id, rendered)
		}
	}
}

func TestUpdateAttrReportsSortedAttributeNames(t *testing.T) {
	doc := testDocument(t, `<html><body><button disabled class="old">Save</button></body></html>`)

	results, totalAffected, opsApplied, _ := ApplyOperations(doc, []Operation{
		{
			Type:     "update_attr",
			Selector: "button",
			Attributes: map[string]any{
				"zeta":      "last",
				"aria-busy": true,
				"class":     nil,
				"data-id":   7,
				"disabled":  false,
			},
		},
	})
	if totalAffected != 1 || opsApplied != 1 {
		t.Fatalf("expected one affected update_attr operation, total=%d applied=%d", totalAffected, opsApplied)
	}
	if len(results) != 1 {
		t.Fatalf("results=%d want 1", len(results))
	}

	wantSet := []string{"aria-busy", "data-id", "zeta"}
	wantRemoved := []string{"class", "disabled"}
	if !reflect.DeepEqual(results[0].AttributesSet, wantSet) {
		t.Fatalf("AttributesSet=%v want %v", results[0].AttributesSet, wantSet)
	}
	if !reflect.DeepEqual(results[0].AttributesRemoved, wantRemoved) {
		t.Fatalf("AttributesRemoved=%v want %v", results[0].AttributesRemoved, wantRemoved)
	}
}

func TestGeneratedNthSelectionAffectsOnlyValidOneIndexedPositions(t *testing.T) {
	cfg := &quick.Config{MaxCount: 100}

	err := quick.Check(func(rawMatches uint8, rawNth uint8) bool {
		matchCount := int(rawMatches%12) + 1
		nth := int(rawNth % 16)
		var html strings.Builder
		html.WriteString("<html><body>")
		for i := 0; i < matchCount; i++ {
			html.WriteString(`<span class="item">x</span>`)
		}
		html.WriteString("</body></html>")

		doc := testDocument(t, html.String())
		results, totalAffected, opsApplied, _ := ApplyOperations(doc, []Operation{
			{Type: "delete", Selector: ".item", Nth: nth},
		})

		wantAffected := 0
		if nth > 0 && nth <= matchCount {
			wantAffected = 1
		} else if nth == 0 {
			wantAffected = matchCount
		}
		if totalAffected != wantAffected || opsApplied != boolToInt(wantAffected > 0) {
			t.Logf("matchCount=%d nth=%d total=%d applied=%d results=%+v", matchCount, nth, totalAffected, opsApplied, results)
			return false
		}

		remaining := doc.Find(".item").Length()
		if remaining != matchCount-wantAffected {
			t.Logf("matchCount=%d nth=%d remaining=%d want %d", matchCount, nth, remaining, matchCount-wantAffected)
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("nth selection property failed: %v", err)
	}
}

func TestGeneratedLimitSelectionAffectsOnlyPrefixMatches(t *testing.T) {
	cfg := &quick.Config{MaxCount: 100}

	err := quick.Check(func(rawMatches uint8, rawLimit uint8) bool {
		matchCount := int(rawMatches%12) + 1
		limit := int(rawLimit % 16)
		var html strings.Builder
		html.WriteString("<html><body>")
		for i := 0; i < matchCount; i++ {
			html.WriteString(`<span class="item">x</span>`)
		}
		html.WriteString("</body></html>")

		doc := testDocument(t, html.String())
		results, totalAffected, opsApplied, _ := ApplyOperations(doc, []Operation{
			{
				Type:       "update_attr",
				Selector:   ".item",
				Limit:      limit,
				Attributes: map[string]any{"data-marked": "yes"},
			},
		})

		wantAffected := matchCount
		if limit > 0 && limit < matchCount {
			wantAffected = limit
		}
		if totalAffected != wantAffected || opsApplied != boolToInt(wantAffected > 0) {
			t.Logf("matchCount=%d limit=%d total=%d applied=%d results=%+v", matchCount, limit, totalAffected, opsApplied, results)
			return false
		}

		ok := true
		doc.Find(".item").Each(func(i int, s *goquery.Selection) {
			_, marked := s.Attr("data-marked")
			if marked != (i < wantAffected) {
				ok = false
			}
		})
		if !ok {
			t.Logf("matchCount=%d limit=%d did not mark exactly the selected prefix", matchCount, limit)
		}
		return ok
	}, cfg)
	if err != nil {
		t.Fatalf("limit selection property failed: %v", err)
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
