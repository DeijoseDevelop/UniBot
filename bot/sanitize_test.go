package bot

import "testing"

func TestSanitizeHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{`Hola <b>mundo</b>`, `Hola <b>mundo</b>`},
		{`a < b y c > d`, `a &lt; b y c &gt; d`},
		{`5 & 3`, `5 &amp; 3`},
		{`<table><thead><tr><th>Curso</th></tr></thead><tbody><tr><td>Física</td></tr></tbody></table>`,
			`<table><thead><tr><th>Curso</th></tr></thead><tbody><tr><td>Física</td></tr></tbody></table>`},
		{`<a href="https://x.com?a=1&b=2">ver</a>`, `<a href="https://x.com?a=1&b=2">ver</a>`},
		{`**texto** y #hash`, `**texto** y #hash`},
		{`<b>a</b> < x`, `<b>a</b> &lt; x`},
	}
	for _, c := range cases {
		got := sanitizeHTML(c.in)
		if got != c.want {
			t.Errorf("sanitizeHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
