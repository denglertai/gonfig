package cmd

import (
	"bytes"
	"io"
	"os"
	"path"
	"testing"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

func Test(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	testCases := []struct {
		desc     string
		file     string
		fileType general.FileType
		wantErr  bool
	}{
		{
			desc:     "XML AutoDiscover",
			file:     path.Join(wd, "./testdata/xml/customers_param.xml"),
			fileType: general.Undefined,
		},
		{
			desc:     "XML Explicit",
			file:     path.Join(wd, "./testdata/xml/customers_param.xml"),
			fileType: general.XML,
		},
		{
			desc:     "JSON AutoDiscover",
			file:     path.Join(wd, "./testdata/json/quiz_param.json"),
			fileType: general.Undefined,
		},
		{
			desc:     "JSON Explicit",
			file:     path.Join(wd, "./testdata/json/quiz_param.json"),
			fileType: general.JSON,
		},
		{
			desc:     "YAML AutoDiscover",
			file:     path.Join(wd, "./testdata/yaml/deployment_param.yaml"),
			fileType: general.Undefined,
		},
		{
			desc:     "YAML Explicit",
			file:     path.Join(wd, "./testdata/yaml/deployment_param.yaml"),
			fileType: general.YAML,
		},
		{
			desc:     "PROPERTIES AutoDiscover",
			file:     path.Join(wd, "./testdata/properties/props_param.properties"),
			fileType: general.Undefined,
		},
		{
			desc:     "PROPERTIES Explicit",
			file:     path.Join(wd, "./testdata/properties/props_param.properties"),
			fileType: general.PROPERTIES,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			old := os.Stdout // keep backup of the real stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			outC := make(chan string)
			// copy the output in a separate goroutine so printing can't block indefinitely
			go func() {
				var buf bytes.Buffer
				io.Copy(&buf, r)
				outC <- buf.String()
			}()

			// BLA_BLUB is expected to be present in the environment variables and the value should be YOYOYO after the command is executed
			t.Setenv("BLA_BLUB", "yoyoyo")
			t.Setenv("INT", "123")
			t.Setenv("FLOAT", "123.123")
			t.Setenv("BOOL", "true")
			t.Setenv("STRING", "string")
			t.Setenv("SPECIAL_CHARACTERS", "%^&*()_+")

			// Take the path of the file and pass it as an argument to the command and see if it works
			args := []string{"config", "process", "-f", tC.file, "-o", "-"}

			if tC.fileType != general.Undefined {
				args = append(args, "-t", string(tC.fileType))
			}

			rootCmd.SetArgs(args)
			err = rootCmd.Execute()
			assert.NoError(t, err)

			// back to normal state
			w.Close()
			os.Stdout = old // restoring the real stdout
			out := <-outC

			snaps.MatchSnapshot(t, out)
		})
	}
}
