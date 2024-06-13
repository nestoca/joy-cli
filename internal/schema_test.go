package internal

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/require"
)

func TestValidateAgainstSchema(t *testing.T) {
	cases := []struct {
		Name   string
		Schema cue.Value
		Input  any
		Err    string
	}{
		{
			Name:   "is valid",
			Schema: cuecontext.New().CompileString(`hello: string`),
			Input:  map[string]string{"hello": "world"},
		},
		{
			Name:   "is invalid",
			Schema: cuecontext.New().CompileString(`string`),
			Input:  3.14159265,
      Err:    "error: conflicting values string and 3.14159265 (mismatched types string and float)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			err := ValidateAgainstSchema(tc.Schema, tc.Input)
			if tc.Err == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.Err)
		})
	}
}
