package plot

import (
	"fmt"
	"io"

	"golang.org/x/crypto/blake2b"
)

// chunkReadSize we'll read the chunk to hash `chunkReadSize` bytes at a time
const chunkReadSize = 100 * 1000 * 1000

// calculateChunkHash calculates the hash of the given chunk, which is given as
// an `io.Reader`. Currently using `blake2`
func calculateChunkHash(chunk io.Reader) (string, error) {
	h, err := blake2b.New512(nil)
	if err != nil {
		return "", err
	}
	buffer := make([]byte, chunkReadSize)
	for {
		r, err := chunk.Read(buffer)
		if err == io.EOF {
			break
		}

		if err != nil {
			return "", err
		}

		if r > 0 {
			h.Write(buffer[:r])
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
