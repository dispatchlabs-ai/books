package banking

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"unicode/utf8"
)

// This tree is intentionally bounded before allocation and never resolves XML
// resources. Adapters validate their own namespaces and financial structure.
type xmlNode struct {
	Name     xml.Name
	Attrs    []xml.Attr
	Text     string
	Children []*xmlNode
}

func xmlTree(data []byte) (*xmlNode, error) {
	if !utf8.Valid(data) {
		return nil, invalid("IMPORT_ENCODING_INVALID", "XML must be UTF-8")
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	var root *xmlNode
	stack := []*xmlNode{}
	count := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, invalid("IMPORT_XML_INVALID", "malformed XML")
		}
		switch v := tok.(type) {
		case xml.StartElement:
			count++
			if count > MaxNodes || len(stack) >= MaxDepth || len(v.Name.Local) > 128 || len(v.Attr) > 32 {
				return nil, invalid("IMPORT_LIMIT_EXCEEDED", "XML exceeds structural limits")
			}
			attrs := map[xml.Name]bool{}
			for _, a := range v.Attr {
				if attrs[a.Name] || len(a.Value) > MaxText {
					return nil, invalid("IMPORT_XML_INVALID", "invalid or repeated XML attribute")
				}
				attrs[a.Name] = true
			}
			n := &xmlNode{Name: v.Name, Attrs: v.Attr}
			if len(stack) == 0 {
				if root != nil {
					return nil, invalid("IMPORT_XML_INVALID", "multiple roots")
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, invalid("IMPORT_XML_INVALID", "unexpected closing tag")
			}
			n := stack[len(stack)-1]
			if len(n.Children) > 0 && strings.TrimSpace(n.Text) != "" {
				return nil, invalid("IMPORT_XML_INVALID", "mixed element text")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(v)) != "" {
					return nil, invalid("IMPORT_XML_INVALID", "text outside root")
				}
				continue
			}
			n := stack[len(stack)-1]
			if len(n.Text)+len(v) > MaxText {
				return nil, invalid("IMPORT_LIMIT_EXCEEDED", "XML field exceeds the text limit")
			}
			n.Text += string(v)
		case xml.Directive:
			return nil, invalid("IMPORT_XML_UNSUPPORTED", "XML directives and external entities are unsupported")
		case xml.ProcInst:
			if v.Target != "xml" || root != nil {
				return nil, invalid("IMPORT_XML_UNSUPPORTED", "unsupported XML processing instruction")
			}
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, invalid("IMPORT_XML_INVALID", "incomplete XML")
	}
	return root, nil
}
func (n *xmlNode) all(name string) []*xmlNode {
	var out []*xmlNode
	for _, c := range n.Children {
		if c.Name.Local == name {
			out = append(out, c)
		}
	}
	return out
}
func (n *xmlNode) one(name string) (*xmlNode, error) {
	found := n.all(name)
	if len(found) != 1 {
		return nil, invalid("IMPORT_XML_FIELD_INVALID", "expected one "+name)
	}
	return found[0], nil
}
func (n *xmlNode) optional(name string) (*xmlNode, error) {
	found := n.all(name)
	if len(found) > 1 {
		return nil, invalid("IMPORT_XML_FIELD_INVALID", "repeated "+name)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0], nil
}
func (n *xmlNode) value(name string) (string, error) {
	child, e := n.one(name)
	if e != nil {
		return "", e
	}
	if len(child.Children) > 0 || strings.TrimSpace(child.Text) == "" {
		return "", invalid("IMPORT_XML_FIELD_INVALID", "expected text in "+name)
	}
	return strings.TrimSpace(child.Text), nil
}
func (n *xmlNode) optionalValue(name string) (string, error) {
	child, e := n.optional(name)
	if e != nil || child == nil {
		return "", e
	}
	if len(child.Children) > 0 {
		return "", invalid("IMPORT_XML_FIELD_INVALID", "expected text in "+name)
	}
	return strings.TrimSpace(child.Text), nil
}
func (n *xmlNode) attr(name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name && a.Name.Space == "" {
			return a.Value
		}
	}
	return ""
}
func (n *xmlNode) path(names ...string) (*xmlNode, error) {
	var e error
	for _, name := range names {
		n, e = n.one(name)
		if e != nil {
			return nil, e
		}
	}
	return n, nil
}
func (n *xmlNode) namespace(space string) bool {
	if n.Name.Space != space {
		return false
	}
	for _, c := range n.Children {
		if !c.namespace(space) {
			return false
		}
	}
	return true
}
func (n *xmlNode) leaves(prefix string, out map[string]string) {
	if len(n.Children) == 0 {
		out[prefix] = strings.TrimSpace(n.Text)
		for _, a := range n.Attrs {
			if a.Name.Space != "xmlns" && a.Name.Local != "xmlns" {
				out[prefix+"/@"+a.Name.Local] = a.Value
			}
		}
		return
	}
	counts := map[string]int{}
	for _, c := range n.Children {
		counts[c.Name.Local]++
	}
	seen := map[string]int{}
	for _, c := range n.Children {
		seen[c.Name.Local]++
		name := c.Name.Local
		if counts[name] > 1 {
			name += ":" + strconvItoa(seen[c.Name.Local])
		}
		c.leaves(prefix+"/"+name, out)
	}
}

func (n *xmlNode) attrNS(space, name string) string {
	for _, a := range n.Attrs {
		if a.Name.Space == space && a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
