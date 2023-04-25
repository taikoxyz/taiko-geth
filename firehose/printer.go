package firehose

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

type Printer interface {
	Print(input ...string)
}

type DelegateToWriterPrinter struct {
	writer io.Writer
}

func (p *DelegateToWriterPrinter) Disabled() bool {
	return false
}

func (p *DelegateToWriterPrinter) Print(input ...string) {
	line := "FIRE " + strings.Join(input, " ") + "\n"
	var written int
	var err error
	loops := 10
	for i := 0; i < loops; i++ {
		written, err = fmt.Fprint(p.writer, line)

		if len(line) == written {
			return
		}

		line = line[written:]

		if i == loops-1 {
			break
		}
	}

	errstr := fmt.Sprintf("\nFIREHOSE FAILED WRITING %dx: %s\n", loops, err)
	ioutil.WriteFile("/tmp/firehose_writer_failed_print.log", []byte(errstr), 0644)
	fmt.Fprint(p.writer, errstr)
}

type ToBufferPrinter struct {
	buffer *bytes.Buffer
}

func NewToBufferPrinter() *ToBufferPrinter {
	return &ToBufferPrinter{
		buffer: bytes.NewBuffer(nil),
	}
}

func (p *ToBufferPrinter) Disabled() bool {
	return false
}

func (p *ToBufferPrinter) Print(input ...string) {
	p.buffer.WriteString("DMLOG " + strings.Join(input, " ") + "\n")
}

func (p *ToBufferPrinter) Buffer() *bytes.Buffer {
	return p.buffer
}

func Addr(in common.Address) string {
	return hex.EncodeToString(in[:])
}

func Bool(in bool) string {
	if in {
		return "true"
	}

	return "false"
}

func Hash(in common.Hash) string {
	return hex.EncodeToString(in[:])
}

func Hex(in []byte) string {
	if len(in) == 0 {
		return "."
	}

	return hex.EncodeToString(in)
}

func BigInt(in *big.Int) string {
	return Hex(in.Bytes())
}

func Uint(in uint) string {
	return strconv.FormatUint(uint64(in), 10)
}

func Uint8(in uint8) string {
	return strconv.FormatUint(uint64(in), 10)
}

func Uint64(in uint64) string {
	return strconv.FormatUint(in, 10)
}

func JSON(in interface{}) string {
	out, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}

	return string(out)
}
