package htmledit

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Operation describes a single HTML edit operation.
type Operation struct {
	Type       string         `json:"type"`                 // select, insert, replace, update_attr, delete, wrap, unwrap, structure, extract
	Selector   string         `json:"selector"`             // CSS selector (optional for structure - defaults to root)
	Position   string         `json:"position,omitempty"`   // before, after, prepend, append (for insert)
	HTML       string         `json:"html,omitempty"`       // HTML content
	Inner      bool           `json:"inner,omitempty"`      // Replace inner HTML only (for replace)
	Attributes map[string]any `json:"attributes,omitempty"` // Attributes to set/remove (for update_attr)
	Nth        int            `json:"nth,omitempty"`        // Target nth match only (1-indexed)
	Limit      int            `json:"limit,omitempty"`      // Max elements to affect
	// Structure operation options
	MaxDepth int `json:"max_depth,omitempty"` // Max depth for structure tree (0 = unlimited)
	// Extract operation options
	IncludeParent int `json:"include_parent,omitempty"` // Include N parent levels for context
	MaxLength     int `json:"max_length,omitempty"`     // Max length per extracted element (0 = unlimited)
}

// OperationResult captures the outcome of an operation.
type OperationResult struct {
	Type              string   `json:"type"`
	Selector          string   `json:"selector"`
	MatchedCount      int      `json:"matched_count"`
	AffectedCount     int      `json:"affected_count"`
	MatchedTags       []string `json:"matched_tags,omitempty"`
	MatchedIDs        []string `json:"matched_ids,omitempty"`
	MatchedClasses    []string `json:"matched_classes,omitempty"`
	TextPreviews      []string `json:"text_previews,omitempty"`
	AttributesSet     []string `json:"attributes_set,omitempty"`
	AttributesRemoved []string `json:"attributes_removed,omitempty"`
	Structure         string   `json:"structure,omitempty"`
	ExtractedHTML     []string `json:"extracted_html,omitempty"`
	Truncated         bool     `json:"truncated,omitempty"`
	Error             string   `json:"error,omitempty"`
}

// ApplyOperations applies each operation to the document and aggregates results.
func ApplyOperations(doc *goquery.Document, ops []Operation) ([]OperationResult, int, int, bool) {
	results := make([]OperationResult, 0, len(ops))
	totalAffected := 0
	opsApplied := 0
	allReadOnly := true

	for _, op := range ops {
		if op.Type != "select" && op.Type != "structure" && op.Type != "extract" {
			allReadOnly = false
			break
		}
	}

	for _, op := range ops {
		result, err := applyOperation(doc, op)
		if err != nil {
			result.Error = err.Error()
		} else if result.AffectedCount > 0 {
			opsApplied++
			totalAffected += result.AffectedCount
		}
		results = append(results, result)
	}

	return results, totalAffected, opsApplied, allReadOnly
}

// RenderDocument renders the document, optionally pretty-printing it.
func RenderDocument(doc *goquery.Document, format bool) (string, error) {
	var buf bytes.Buffer

	htmlContent, err := doc.Html()
	if err != nil {
		return "", err
	}

	if format {
		node, err := html.Parse(strings.NewReader(htmlContent))
		if err != nil {
			return "", err
		}
		if err := prettyPrint(&buf, node, 0); err != nil {
			return "", err
		}
		return buf.String(), nil
	}

	return htmlContent, nil
}

func applyOperation(doc *goquery.Document, op Operation) (OperationResult, error) {
	result := OperationResult{
		Type:     op.Type,
		Selector: op.Selector,
	}

	if op.Type == "structure" {
		result.Structure = applyStructure(doc, op)
		result.AffectedCount = 1
		return result, nil
	}

	if op.Selector == "" {
		return result, errors.New("selector is required")
	}

	selection := doc.Find(op.Selector)
	result.MatchedCount = selection.Length()
	if result.MatchedCount == 0 {
		return result, nil
	}

	result.MatchedTags = make([]string, 0)
	result.MatchedIDs = make([]string, 0)
	result.MatchedClasses = make([]string, 0)
	result.TextPreviews = make([]string, 0)

	selection.Each(func(i int, s *goquery.Selection) {
		if len(result.MatchedTags) < 5 {
			if node := s.Nodes[0]; node != nil {
				result.MatchedTags = append(result.MatchedTags, node.Data)
			}
		}
		if id, exists := s.Attr("id"); exists && len(result.MatchedIDs) < 5 {
			result.MatchedIDs = append(result.MatchedIDs, id)
		}
		if class, exists := s.Attr("class"); exists && len(result.MatchedClasses) < 5 {
			result.MatchedClasses = append(result.MatchedClasses, class)
		}
		if len(result.TextPreviews) < 3 {
			text := strings.TrimSpace(s.Text())
			if len(text) > 50 {
				text = text[:50] + "..."
			}
			if text != "" {
				result.TextPreviews = append(result.TextPreviews, text)
			}
		}
	})

	selection = filterSelection(selection, op.Nth, op.Limit)

	switch op.Type {
	case "select":
		result.AffectedCount = selection.Length()

	case "insert":
		count, err := applyInsert(selection, op)
		if err != nil {
			return result, err
		}
		result.AffectedCount = count

	case "replace":
		count, err := applyReplace(selection, op)
		if err != nil {
			return result, err
		}
		result.AffectedCount = count

	case "update_attr":
		count, attrSet, attrRemoved, err := applyUpdateAttr(selection, op)
		if err != nil {
			return result, err
		}
		result.AffectedCount = count
		result.AttributesSet = attrSet
		result.AttributesRemoved = attrRemoved

	case "delete":
		result.AffectedCount = applyDelete(selection)

	case "wrap":
		count, err := applyWrap(selection, op)
		if err != nil {
			return result, err
		}
		result.AffectedCount = count

	case "unwrap":
		result.AffectedCount = applyUnwrap(selection)

	case "extract":
		extracted, truncated := applyExtract(selection, op)
		result.ExtractedHTML = extracted
		result.Truncated = truncated
		result.AffectedCount = len(extracted)

	default:
		return result, fmt.Errorf("unknown operation type: %s", op.Type)
	}

	return result, nil
}

func filterSelection(selection *goquery.Selection, nth, limit int) *goquery.Selection {
	if nth > 0 {
		if nth <= selection.Length() {
			return selection.Eq(nth - 1)
		}
		return selection.Eq(-1)
	}
	if limit > 0 && limit < selection.Length() {
		return selection.Slice(0, limit)
	}
	return selection
}

func applyInsert(selection *goquery.Selection, op Operation) (int, error) {
	if op.HTML == "" {
		return 0, errors.New("html is required for insert operation")
	}
	if op.Position == "" {
		return 0, errors.New("position is required for insert operation")
	}

	count := 0
	var insertErr error

	selection.Each(func(_ int, s *goquery.Selection) {
		if insertErr != nil {
			return
		}

		switch op.Position {
		case "before":
			s.BeforeHtml(op.HTML)
			count++
		case "after":
			s.AfterHtml(op.HTML)
			count++
		case "prepend":
			s.PrependHtml(op.HTML)
			count++
		case "append":
			s.AppendHtml(op.HTML)
			count++
		default:
			insertErr = fmt.Errorf("invalid position: %s", op.Position)
		}
	})

	return count, insertErr
}

func applyReplace(selection *goquery.Selection, op Operation) (int, error) {
	if op.HTML == "" {
		return 0, errors.New("html is required for replace operation")
	}

	count := 0
	selection.Each(func(_ int, s *goquery.Selection) {
		if op.Inner {
			s.SetHtml(op.HTML)
		} else {
			s.ReplaceWithHtml(op.HTML)
		}
		count++
	})

	return count, nil
}

func applyUpdateAttr(selection *goquery.Selection, op Operation) (int, []string, []string, error) {
	if len(op.Attributes) == 0 {
		return 0, nil, nil, errors.New("attributes map is required for update_attr operation")
	}

	count := 0
	var attrSet, attrRemoved []string

	selection.Each(func(i int, s *goquery.Selection) {
		for key, value := range op.Attributes {
			if value == nil {
				s.RemoveAttr(key)
				if i == 0 {
					attrRemoved = append(attrRemoved, key)
				}
			} else {
				var strValue string
				switch v := value.(type) {
				case string:
					strValue = v
				case bool:
					if v {
						strValue = key
					} else {
						s.RemoveAttr(key)
						if i == 0 {
							attrRemoved = append(attrRemoved, key)
						}
						continue
					}
				default:
					strValue = fmt.Sprintf("%v", v)
				}
				s.SetAttr(key, strValue)
				if i == 0 {
					attrSet = append(attrSet, key)
				}
			}
		}
		count++
	})

	return count, attrSet, attrRemoved, nil
}

func applyDelete(selection *goquery.Selection) int {
	count := selection.Length()
	selection.Remove()
	return count
}

func applyWrap(selection *goquery.Selection, op Operation) (int, error) {
	if op.HTML == "" {
		return 0, errors.New("html is required for wrap operation")
	}

	count := 0
	selection.Each(func(_ int, s *goquery.Selection) {
		s.WrapHtml(op.HTML)
		count++
	})

	return count, nil
}

func applyUnwrap(selection *goquery.Selection) int {
	count := 0
	selection.Each(func(_ int, s *goquery.Selection) {
		s.Unwrap()
		count++
	})
	return count
}

func applyStructure(doc *goquery.Document, op Operation) string {
	var buf bytes.Buffer

	var startNode *html.Node
	if op.Selector != "" {
		selection := doc.Find(op.Selector)
		if selection.Length() > 0 {
			startNode = selection.Nodes[0]
		}
	}

	if startNode == nil {
		htmlSel := doc.Find("html")
		if htmlSel.Length() > 0 {
			startNode = htmlSel.Nodes[0]
		} else {
			for c := doc.Nodes[0]; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode {
					startNode = c
					break
				}
			}
		}
	}

	if startNode == nil {
		return "(empty document)"
	}

	maxDepth := op.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 10
	}

	buildStructureTree(&buf, startNode, "", true, 0, maxDepth)
	return strings.TrimRight(buf.String(), "\n")
}

func buildStructureTree(buf *bytes.Buffer, node *html.Node, prefix string, isLast bool, depth, maxDepth int) {
	if node == nil || node.Type != html.ElementNode {
		return
	}
	if depth >= maxDepth {
		return
	}

	label := node.Data
	if id := getAttr(node, "id"); id != "" {
		label += "#" + id
	}
	if class := getAttr(node, "class"); class != "" {
		classes := strings.Fields(class)
		if len(classes) > 3 {
			label += "." + strings.Join(classes[:3], ".") + "..."
		} else {
			label += "." + strings.Join(classes, ".")
		}
	}

	connector := "├── "
	if isLast {
		connector = "└── "
	}
	if depth == 0 {
		connector = ""
	}

	buf.WriteString(prefix)
	buf.WriteString(connector)
	buf.WriteString(label)

	children := collectElementChildren(node)
	grouped := groupSiblings(children)

	if len(grouped) > 0 && depth+1 < maxDepth {
		buf.WriteString("\n")
		childPrefix := prefix
		if depth > 0 {
			if isLast {
				childPrefix += "    "
			} else {
				childPrefix += "│   "
			}
		}

		for i, group := range grouped {
			isLastChild := i == len(grouped)-1
			if group.count > 1 {
				buf.WriteString(childPrefix)
				if isLastChild {
					buf.WriteString("└── ")
				} else {
					buf.WriteString("├── ")
				}
				fmt.Fprintf(buf, "%s (%d)\n", group.label, group.count)
			} else {
				buildStructureTree(buf, group.node, childPrefix, isLastChild, depth+1, maxDepth)
			}
		}
	} else if len(children) > 0 {
		fmt.Fprintf(buf, " (%d children)\n", len(children))
	} else {
		buf.WriteString("\n")
	}
}

type siblingGroup struct {
	node  *html.Node
	label string
	count int
}

func groupSiblings(children []*html.Node) []siblingGroup {
	if len(children) == 0 {
		return nil
	}

	var groups []siblingGroup
	for i := 0; i < len(children); {
		node := children[i]
		label := node.Data
		nodeID := getAttr(node, "id")
		nodeClass := getAttr(node, "class")

		if nodeID != "" {
			label += "#" + nodeID
		}
		if nodeClass != "" {
			classes := strings.Fields(nodeClass)
			if len(classes) > 2 {
				label += "." + strings.Join(classes[:2], ".") + "..."
			} else {
				label += "." + strings.Join(classes, ".")
			}
		}

		count := 1
		canGroup := nodeID == ""
		if canGroup {
			for j := i + 1; j < len(children); j++ {
				nextNode := children[j]
				nextID := getAttr(nextNode, "id")
				nextClass := getAttr(nextNode, "class")
				if nextNode.Data == node.Data && nextID == "" && nextClass == nodeClass {
					count++
				} else {
					break
				}
			}
		}

		if count > 1 {
			groups = append(groups, siblingGroup{node: node, label: label, count: count})
		} else {
			groups = append(groups, siblingGroup{node: node, label: label, count: 1})
		}
		i += count
	}
	return groups
}

func collectElementChildren(node *html.Node) []*html.Node {
	var children []*html.Node
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			children = append(children, c)
		}
	}
	return children
}

func getAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func applyExtract(selection *goquery.Selection, op Operation) ([]string, bool) {
	var extracted []string
	truncated := false

	maxLength := op.MaxLength
	if maxLength <= 0 {
		maxLength = 10000
	}

	limit := op.Limit
	if limit <= 0 {
		limit = 10
	}

	selection.Each(func(i int, s *goquery.Selection) {
		if i >= limit {
			return
		}

		target := s
		if op.IncludeParent > 0 {
			for j := 0; j < op.IncludeParent; j++ {
				parent := target.Parent()
				if parent.Length() > 0 && goquery.NodeName(parent) != "" && goquery.NodeName(parent) != "#document" {
					target = parent
				} else {
					break
				}
			}
		}

		htmlContent, err := goquery.OuterHtml(target)
		if err != nil {
			return
		}

		if len(htmlContent) > maxLength {
			htmlContent = htmlContent[:maxLength] + "\n... (truncated)"
			truncated = true
		}

		extracted = append(extracted, htmlContent)
	})

	return extracted, truncated
}

func prettyPrint(w *bytes.Buffer, n *html.Node, indent int) error {
	switch n.Type {
	case html.DocumentNode:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := prettyPrint(w, c, indent); err != nil {
				return err
			}
		}

	case html.DoctypeNode:
		w.WriteString("<!DOCTYPE ")
		w.WriteString(n.Data)
		w.WriteString(">\n")

	case html.ElementNode:
		isVoid := isVoidElement(n.Data)
		preserveWS := preservesWhitespace(n.Data)
		isInline := isInlineElement(n.Data)

		if !isInline {
			writeIndent(w, indent)
		}

		w.WriteString("<")
		w.WriteString(n.Data)

		for _, attr := range n.Attr {
			w.WriteString(" ")
			w.WriteString(attr.Key)
			w.WriteString(`="`)
			w.WriteString(html.EscapeString(attr.Val))
			w.WriteString(`"`)
		}

		if isVoid {
			w.WriteString(">")
			if !isInline {
				w.WriteString("\n")
			}
			return nil
		}

		w.WriteString(">")

		hasChildren := n.FirstChild != nil
		hasOnlyText := hasChildren && n.FirstChild == n.LastChild && n.FirstChild.Type == html.TextNode

		if hasOnlyText && !preserveWS {
			text := strings.TrimSpace(n.FirstChild.Data)
			if text != "" {
				w.WriteString(text)
			}
		} else if hasChildren {
			if !isInline && !preserveWS {
				w.WriteString("\n")
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if preserveWS {
					if err := html.Render(w, c); err != nil {
						return err
					}
				} else {
					if err := prettyPrint(w, c, indent+1); err != nil {
						return err
					}
				}
			}
			if !isInline && !preserveWS && !hasOnlyText {
				writeIndent(w, indent)
			}
		}

		w.WriteString("</")
		w.WriteString(n.Data)
		w.WriteString(">")
		if !isInline {
			w.WriteString("\n")
		}

	case html.TextNode:
		text := strings.TrimSpace(n.Data)
		if text != "" {
			writeIndent(w, indent)
			w.WriteString(text)
			w.WriteString("\n")
		}

	case html.CommentNode:
		writeIndent(w, indent)
		w.WriteString("<!--")
		w.WriteString(n.Data)
		w.WriteString("-->")
		w.WriteString("\n")
	}

	return nil
}

func writeIndent(w *bytes.Buffer, indent int) {
	for i := 0; i < indent; i++ {
		w.WriteString("  ")
	}
}

func isVoidElement(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

func isInlineElement(tag string) bool {
	switch tag {
	case "a", "abbr", "b", "bdo", "br", "cite", "code", "dfn", "em",
		"i", "img", "kbd", "q", "samp", "small", "span", "strong",
		"sub", "sup", "time", "var":
		return true
	}
	return false
}

func preservesWhitespace(tag string) bool {
	switch tag {
	case "pre", "code", "textarea", "script", "style":
		return true
	}
	return false
}
