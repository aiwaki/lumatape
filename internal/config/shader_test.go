package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestShaderConfigRoundTripAndStrictArray(t *testing.T) {
	c := Default()
	c.Mode = "full"
	c.Target = Target{Kind: "window", WindowTitle: "game"}
	c.Shader = ShaderConfig{ID: strings.Repeat("a", 64), Params: [8]float64{.5, 2, -1}}
	data, _ := json.Marshal(c)
	got, err := Decode(data)
	if err != nil || got != c {
		t.Fatalf("roundtrip %+v %v", got, err)
	}
	for _, bad := range []string{"[]", "[0]", "[0,0,0,0,0,0,0,0,0]", "[0,0,0,0,0,0,0,null]", "[0,0,0,0,0,0,0,{}]"} {
		if _, err := Decode([]byte(`{"shader":{"params":` + bad + `}}`)); err == nil {
			t.Fatalf("accepted malformed params %s", bad)
		}
	}
	if _, err := Decode([]byte(`{"effects":[]}`)); err == nil {
		t.Fatal("accepted unrelated array")
	}
	if _, err := Decode([]byte(`{"shader":{"params":[0,0,0,0,0,0,0,0],"Params":[0,0,0,0,0,0,0,0]}}`)); err == nil {
		t.Fatal("accepted duplicate array alias")
	}
	for _, bad := range []string{"../shader", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		c.Shader.ID = bad
		if c.Validate() == nil {
			t.Fatalf("accepted id %s", bad)
		}
	}
}
func TestCustomRequiresFullEvenWhenDisabled(t *testing.T) {
	c := Default()
	c.Shader.ID = strings.Repeat("b", 64)
	for _, enabled := range []bool{false, true} {
		c.Enabled = enabled
		if c.Validate() == nil {
			t.Fatal("custom overlay accepted")
		}
	}
	c.Shader = ShaderConfig{}
	c.Shader.Params[3] = .5
	if c.Validate() == nil {
		t.Fatal("builtin retained hidden custom parameter")
	}
}
