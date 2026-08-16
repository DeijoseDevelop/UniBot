package bot

import "testing"

func TestMarkdownToHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		// Negritas markdown (el caso reportado por el usuario)
		{"Tienes **6 cursos** en total", "Tienes <b>6 cursos</b> en total"},
		// Cursivas
		{"texto _importante_ y *otro*", "texto <i>importante</i> y <i>otro</i>"},
		// Código inline y en bloque
		{"usa `go run`", "usa <code>go run</code>"},
		{"```\npackage main\n```", "<pre>package main</pre>"},
		// Enlaces
		{"ver [aquí](https://x.com/a)", `ver <a href="https://x.com/a">aquí</a>`},
		// Encabezados
		{"# Título", "<b>Título</b>"},
		// Tachado
		{"~~viejo~~ texto", "<s>viejo</s> texto"},
		// Tabla con pipes → pre alineado
		{"| Curso | Sección |\n|---|---|\n| Física | A |\n| Química | B |",
			"<pre>Curso    Sección\nFísica   A      \nQuímica  B      </pre>"},
		// Respeta <pre> ya emitidos
		{"antes\n<pre>A  B\n1  2</pre>\ndespués", "antes\n<pre>A  B\n1  2</pre>\ndespués"},
	}
	for _, c := range cases {
		got := markdownToHTML(c.in)
		if got != c.want {
			t.Errorf("markdownToHTML(%q) = %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{`Hola <b>mundo</b>`, `Hola <b>mundo</b>`},
		{`a < b y c > d`, `a &lt; b y c &gt; d`},
		{`5 & 3`, `5 &amp; 3`},
		{`<a href="https://x.com?a=1&b=2">ver</a>`, `<a href="https://x.com?a=1&b=2">ver</a>`},
		{`**texto** y #hash`, `**texto** y #hash`},
		{`<b>a</b> < x`, `<b>a</b> &lt; x`},
		{`<table><thead><tr><th>Curso</th><th>Sección</th></tr></thead><tbody><tr><td>Física</td></tr></tbody></table>`,
			"Curso | Sección | \n\nFísica | \n\n\n"},
	}
	for _, c := range cases {
		got := sanitizeHTML(c.in)
		if got != c.want {
			t.Errorf("sanitizeHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
