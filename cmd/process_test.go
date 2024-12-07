package cmd

import (
	"bytes"
	"io"
	"os"
	"path"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

func Test_Config_Process_XML(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

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

	// Take the path of the file and pass it as an argument to the command and see if it works
	args := []string{"config", "process", "-f", path.Join(wd, "../internal/file/testdata/xml/customers_param.xml"), "-o", "-"}
	rootCmd.SetArgs(args)
	err = rootCmd.Execute()
	assert.NoError(t, err)

	// back to normal state
	w.Close()
	os.Stdout = old // restoring the real stdout
	out := <-outC

	snaps.MatchSnapshot(t, out)
}
