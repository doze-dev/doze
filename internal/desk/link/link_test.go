package link

import "testing"

func TestParse(t *testing.T) {
	for in, want := range map[string]Link{
		"doze://shop":                        {Stack: "shop"},
		"doze://shop/db":                     {Stack: "shop", Show: "db"},
		"doze://shop/db/logs":                {Stack: "shop", Show: "db"},
		"DOZE://Library":                     {Page: "library"},
		"doze://settings":                    {Page: "settings"},
		"doze://open?dir=/code/shop&show=db": {Dir: "/code/shop", Show: "db"},
	} {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"https://shop", "doze://", "doze://open?dir=relative", "doze://open"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) took it", bad)
		}
	}
	if !Is("doze://shop") || Is("/code/shop") {
		t.Error("Is")
	}
}
