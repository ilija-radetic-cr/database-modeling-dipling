package llmpipeline

import (
	"reflect"
	"strings"
)

// Serbian text the pipeline writes — descriptions, findings, model labels and
// enum values — is in "ošišana latinica": Latin letters without diacritics,
// whatever script the task text uses. Cyrillic is transliterated by sound
// and č, ć, š, ž, đ become c, c, s, z, dj. The task text itself is never
// rewritten; it stays the evidence the description is checked against.
var asciiSerbianReplacer = strings.NewReplacer(
	"č", "c", "ć", "c", "š", "s", "ž", "z", "đ", "dj", "Č", "C", "Ć", "C", "Š", "S", "Ž", "Z", "Đ", "Dj",
	"а", "a", "б", "b", "в", "v", "г", "g", "д", "d", "ђ", "dj", "е", "e", "ж", "z", "з", "z", "и", "i", "ј", "j",
	"к", "k", "л", "l", "љ", "lj", "м", "m", "н", "n", "њ", "nj", "о", "o", "п", "p", "р", "r", "с", "s", "т", "t",
	"ћ", "c", "у", "u", "ф", "f", "х", "h", "ц", "c", "ч", "c", "џ", "dz", "ш", "s",
	"А", "A", "Б", "B", "В", "V", "Г", "G", "Д", "D", "Ђ", "Dj", "Е", "E", "Ж", "Z", "З", "Z", "И", "I", "Ј", "J",
	"К", "K", "Л", "L", "Љ", "Lj", "М", "M", "Н", "N", "Њ", "Nj", "О", "O", "П", "P", "Р", "R", "С", "S", "Т", "T",
	"Ћ", "C", "У", "U", "Ф", "F", "Х", "H", "Ц", "C", "Ч", "C", "Џ", "Dz", "Ш", "S",
)

func asciiSerbian(value string) string {
	return asciiSerbianReplacer.Replace(value)
}

// writeASCIISerbian rewrites, in place, every string reachable from value (a
// pointer to a struct): fields, list items and nested structs.
func writeASCIISerbian(value any) {
	writeASCIISerbianValue(reflect.ValueOf(value))
}

func writeASCIISerbianValue(value reflect.Value) {
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			writeASCIISerbianValue(value.Elem())
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if value.Type().Field(i).IsExported() {
				writeASCIISerbianValue(value.Field(i))
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			writeASCIISerbianValue(value.Index(i))
		}
	case reflect.String:
		if value.CanSet() {
			value.SetString(asciiSerbian(value.String()))
		}
	}
}
