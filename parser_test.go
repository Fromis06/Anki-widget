package main

import "testing"

func TestParseCardHTMLRubyBreakImage(t *testing.T) {
	in := `775<br/><ruby>貯<rp>(</rp><rt>たくわ</rt><rp>)</rp></ruby>える &lt;ok&gt;<br/><img src="/media/dict%20note.png"/>`
	nodes := ParseCardHTML(in)
	want := []CardNode{
		{Kind: NodeText, Text: "775"},
		{Kind: NodeBreak},
		{Kind: NodeRuby, Text: "貯", Ruby: "たくわ"},
		{Kind: NodeText, Text: "える <ok>"},
		{Kind: NodeBreak},
		{Kind: NodeImage, Src: "/media/dict%20note.png"},
	}
	if len(nodes) != len(want) {
		t.Fatalf("node count mismatch: got %d (%+v), want %d (%+v)", len(nodes), nodes, len(want), want)
	}
	for i := range want {
		if nodes[i] != want[i] {
			t.Fatalf("node[%d] mismatch: got %+v, want %+v", i, nodes[i], want[i])
		}
	}
}
