package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// Suffix is appended on encryption and removed on decryption from given input
const Suffix = ".aes"

// Encrypter is used to encrypt/decrypt backups
type Encrypter struct {
	key string
	log *zap.SugaredLogger
}

// New creates a new Encrypter with the given key.
// The key should be 16 bytes (AES-128), 24 bytes (AES-192) or
// 32 bytes (AES-256)
func New(log *zap.SugaredLogger, key string) (*Encrypter, error) {
	switch len(key) {
	case 16, 24, 32:
	default:
		return nil, fmt.Errorf("key length:%d invalid, must be 16,24 or 32 bytes", len(key))
	}

	return &Encrypter{
		key: key,
		log: log,
	}, nil
}

// Encrypt input file with key and store the encrypted files with suffix appended
func (e *Encrypter) Encrypt(input string) (string, error) {
	infile, err := os.Open(input)
	if err != nil {
		return "", err
	}
	defer infile.Close()

	key := []byte(e.key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	// Never use more than 2^32 random nonces with a given key
	// because of the risk of repeat.
	iv := make([]byte, block.BlockSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	output := input + Suffix
	outfile, err := os.OpenFile(output, os.O_RDWR|os.O_CREATE, 0777)
	if err != nil {
		return "", err
	}
	defer outfile.Close()

	// The buffer size must be multiple of 16 bytes
	buf := make([]byte, 1024)
	stream := cipher.NewCTR(block, iv)
	for {
		n, err := infile.Read(buf)
		if n > 0 {
			stream.XORKeyStream(buf, buf[:n])
			// Write into file
			_, err = outfile.Write(buf[:n])
			if err != nil {
				return "", err
			}
		}

		if err == io.EOF {
			break
		}

		if err != nil {
			e.log.Infof("Read %d bytes: %v", n, err)
			break
		}
	}
	// Append the IV
	_, err = outfile.Write(iv)
	return output, err
}

// Decrypt input file with key and store decrypted result with suffix removed
// if input does not end with suffix, it is assumed that the file was not encrypted.
func (e *Encrypter) Decrypt(input string) (string, error) {
	extension := filepath.Ext(input)
	if extension != Suffix {
		return input, fmt.Errorf("input is not encrypted")
	}
	infile, err := os.Open(input)
	if err != nil {
		return "", err
	}
	defer infile.Close()

	key := []byte(e.key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	// Never use more than 2^32 random nonces with a given key
	// because of the risk of repeat.
	fi, err := infile.Stat()
	if err != nil {
		return "", err
	}

	iv := make([]byte, block.BlockSize())
	msgLen := fi.Size() - int64(len(iv))
	_, err = infile.ReadAt(iv, msgLen)
	if err != nil {
		return "", err
	}

	output := strings.TrimSuffix(input, Suffix)
	outfile, err := os.OpenFile(output, os.O_RDWR|os.O_CREATE, 0777)
	if err != nil {
		return "", err
	}
	defer outfile.Close()

	// The buffer size must be multiple of 16 bytes
	buf := make([]byte, 1024)
	stream := cipher.NewCTR(block, iv)
	for {
		n, err := infile.Read(buf)
		if n > 0 {
			// The last bytes are the IV, don't belong the original message
			if n > int(msgLen) {
				n = int(msgLen)
			}
			msgLen -= int64(n)
			stream.XORKeyStream(buf, buf[:n])
			// Write into file
			_, err = outfile.Write(buf[:n])
			if err != nil {
				return "", err
			}
		}

		if err == io.EOF {
			break
		}

		if err != nil {
			e.log.Infof("Read %d bytes: %v", n, err)
			break
		}
	}
	return output, nil
}