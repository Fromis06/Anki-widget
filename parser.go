package main

import (
	"io"
	"strings"

	xhtml "golang.org/x/net/html"
)

type CardNodeKind int

const (
	NodeText CardNodeKind = iota
	NodeBreak
	NodeRuby
	NodeImage
)

// CardNode represents a parsed element inside sanitized Anki HTML for native Win32 rendering.
type CardNode struct {
	Kind CardNodeKind
	Text string
	Ruby string
	Src  string
}

// ParseCardHTML parses sanitized Anki HTML (<br/>, <ruby>/<rt>, <img src>, plain text) into native render nodes.
func ParseCardHTML(sanitizedHTML string) []CardNode {
	if strings.TrimSpace(sanitizedHTML) == "" {
		return nil
	}

	z := xhtml.NewTokenizer(strings.NewReader(sanitizedHTML))
	var nodes []CardNode
	inRuby := false
	inRt := false
	inRp := false
	var rubyBase strings.Builder
	var rubyRt strings.Builder

	flushText := func(txt string) {
		if txt == "" {
			return
		}
		if inRp {
			return
		}
		if inRt {
			rubyRt.WriteString(txt)
			return
		}
		if inRuby {
			rubyBase.WriteString(txt)
			return
		}
		if len(nodes) > 0 && nodes[len(nodes)-1].Kind == NodeText {
			nodes[len(nodes)-1].Text += txt
			return
		}
		nodes = append(nodes, CardNode{Kind: NodeText, Text: txt})
	}

	for {
		tt := z.Next()
		switch tt {
		case xhtml.ErrorToken:
			if z.Err() == io.EOF {
				if inRuby {
					nodes = append(nodes, CardNode{
						Kind: NodeRuby,
						Text: rubyBase.String(),
						Ruby: rubyRt.String(),
					})
				}
				return nodes
			}
			return nodes

		case xhtml.TextToken:
			flushText(string(z.Text()))

		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			nameBytes, hasAttr := z.TagName()
			tag := strings.ToLower(string(nameBytes))
			switch tag {
			case "br":
				if inRuby {
					inRuby = false
					inRt = false
					inRp = false
					nodes = append(nodes, CardNode{Kind: NodeRuby, Text: rubyBase.String(), Ruby: rubyRt.String()})
					rubyBase.Reset()
					rubyRt.Reset()
				}
				nodes = append(nodes, CardNode{Kind: NodeBreak})
			case "ruby":
				inRuby = true
				inRt = false
				inRp = false
				rubyBase.Reset()
				rubyRt.Reset()
			case "rt":
				if inRuby {
					inRt = true
				}
			case "rp":
				if inRuby {
					inRp = true
				}
			case "img":
				var src string
				for hasAttr {
					k, v, more := z.TagAttr()
					if strings.EqualFold(string(k), "src") {
						src = strings.TrimSpace(string(v))
					}
					hasAttr = more
				}
				if src != "" {
					nodes = append(nodes, CardNode{Kind: NodeImage, Src: src})
				}
			}

		case xhtml.EndTagToken:
			nameBytes, _ := z.TagName()
			tag := strings.ToLower(string(nameBytes))
			switch tag {
			case "rt":
				inRt = false
			case "rp":
				inRp = false
			case "ruby":
				if inRuby {
					base := strings.TrimSpace(rubyBase.String())
					rt := strings.TrimSpace(rubyRt.String())
					if base != "" {
						if rt != "" {
							nodes = append(nodes, CardNode{Kind: NodeRuby, Text: base, Ruby: rt})
						} else {
							nodes = append(nodes, CardNode{Kind: NodeText, Text: base})
						}
					}
					inRuby = false
					inRt = false
					inRp = false
					rubyBase.Reset()
					rubyRt.Reset()
				}
			}
		}
	}
}

