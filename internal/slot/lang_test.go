package slot

import "testing"

func TestDetectLanguage(t *testing.T) {
	es := `# Guía del proyecto

Este proyecto es una aplicación web para la gestión de tareas. Antes de hacer
cambios, el equipo debe leer la documentación y ejecutar las pruebas con el
comando que se indica más abajo. No se debe subir código sin revisar.

Cuando hay un error en la compilación, hay que avisar a quien mantiene el
repositorio y esperar su respuesta.
`
	en := `# Project guide

This project is a web application for managing tasks. Before you make any
changes, the team should read the documentation and run the tests with the
command that is shown below. Code must not be pushed when it has not been
reviewed.

If there is a build error, you should tell the maintainer of the repository
and wait for the answer.
`
	mixed := `# Notas

El proyecto usa the new pipeline and the old one. Para correr los tests, use the
make target that is in the Makefile. Los cambios que se hacen deben pasar
the review, and the team will check that the build is green. Se debe esperar
la respuesta del equipo antes de que se suba el cambio.
`
	spanishInCode := "# Build\n\n```\nel la los las de del que y en un una es se por para con\n" +
		"el la los las de del que y en un una es se por para con\n```\n" +
		"Run the tests before you push and make sure that the build is green for the team.\n"
	spanishInBlock := "<!-- BEGIN VEXILLUM v:1 hash:00000000 -->\n" +
		"El commander es el que coordina a los soldiers para que hagan el trabajo de la misión con el general.\n" +
		"<!-- END VEXILLUM -->\n" +
		"Run the tests before you push and make sure that the build is green for the team.\n"
	englishInOrphanBlock := "<!-- BEGIN VEXILLUM v:1 hash:00000000 -->\n" +
		"Todo el contenido de este archivo es para que el equipo lo lea antes de que se haga el cambio en la rama.\n"

	tests := []struct {
		name     string
		content  string
		wantLang Lang
		wantConf bool
	}{
		{"empty", "", LangEN, false},
		{"whitespace", "\n\n  \n", LangEN, false},
		{"spanish", es, LangES, true},
		{"english", en, LangEN, true},
		{"mixed is ambiguous", mixed, LangEN, false},
		{"too short", "Usa el comando de la guía.", LangEN, false},
		{"only code and symbols", "```\nmake test\n```\n- `go build`\n", LangEN, false},
		{"spanish inside code fence ignored", spanishInCode, LangEN, true},
		{"vexillum block ignored", spanishInBlock, LangEN, true},
		{"orphan marker line ignored, text counts", englishInOrphanBlock, LangES, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lang, conf := DetectLanguage(tc.content)
			if lang != tc.wantLang {
				t.Errorf("lang = %s (conf %.2f), want %s", lang, conf, tc.wantLang)
			}
			if (conf > 0) != tc.wantConf {
				t.Errorf("confidence = %.2f, want >0 = %v", conf, tc.wantConf)
			}
			if conf < 0 || conf > 1 {
				t.Errorf("confidence out of range: %v", conf)
			}
		})
	}
}

func TestParseLang(t *testing.T) {
	for in, want := range map[string]Lang{"en": LangEN, "ES": LangES, " es ": LangES} {
		if got, err := ParseLang(in); err != nil || got != want {
			t.Errorf("ParseLang(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseLang("fr"); err == nil {
		t.Error("want error for fr")
	}
}
