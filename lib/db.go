package lib

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/tharvik/flock"
)

func packArguments(args []string) []byte {
	toSerialize := make([]interface{}, 1+len(args))

	toSerialize[0] = uint32(len(args))
	for i, arg := range args {
		toSerialize[1+i] = uint32(len(arg))
	}
	for _, arg := range args {
		toSerialize = append(toSerialize, []byte(arg))
	}

	var buf bytes.Buffer
	for _, e := range toSerialize {
		err := binary.Write(&buf, binary.BigEndian, e)
		if err != nil {
			panic(err)
		}
	}

	return buf.Bytes()
}

func unpackArguments(encoded []byte) (elems []string, err error) {
	buf := bytes.NewBuffer(encoded)

	var size uint32
	err = binary.Read(buf, binary.BigEndian, &size)
	if err != nil {
		return
	}

	elems_sizes := make([]uint32, size)
	for i := uint32(0); i < size; i++ {
		err = binary.Read(buf, binary.BigEndian, &elems_sizes[i])
		if err != nil {
			return
		}
	}

	elems = make([]string, size)

	string_pool := buf.Bytes()
	for i, elem_size := range elems_sizes {
		if uint32(len(string_pool)) < elem_size {
			return elems, io.EOF
		}
		elems[i] = string(string_pool[:elem_size])
		string_pool = string_pool[elem_size:]
	}

	return elems, nil
}

func writeArguments(path string, arguments []string) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, os.ModePerm)
	if err != nil {
		return err
	}
	defer file.Close()

	packed := packArguments(arguments)
	buf := make([]byte, len(packed))
	read, err := file.Read(buf)
	if err == io.EOF {
		_, err := file.Write(packed)
		if err != nil {
			return err
		}
	} else if read != len(packed) || !bytes.Equal(packed, buf) {
		return errors.New("other arguments than expected")
	}

	return nil
}

func Init() error {
	for _, dir := range []string{
		ConfigDir,
		filepath.Join(FetchersDir),
		filepath.Join(DownloadsDir),
	} {
		err := os.Mkdir(dir, os.ModePerm)
		if err != nil && !os.IsExist(err) {
			return err
		}
	}

	return nil
}

func AddDownload(dl Download) error {
	// output
	downloadPath := filepath.Join(DownloadsDir, dl.OutputPath)
	err := os.Mkdir(downloadPath, os.ModePerm)
	if err != nil && !os.IsExist(err) {
		return err
	}

	fileLock := flock.New(downloadPath)
	defer fileLock.Close()

	locked, err := fileLock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("already locked")
	}

	// fetcher
	fetcherPath := filepath.Join(FetchersDir, dl.Fetcher.Name)
	fetcherLinkPath := filepath.Join(downloadPath, DownloadFetcherFileName)
	linkRelativePath, err := filepath.Rel(fetcherLinkPath, fetcherPath)
	if err != nil {
		return err
	}

	err = os.Symlink(linkRelativePath, fetcherLinkPath)
	if err != nil {
		if !os.IsExist(err) {
			return err
		}

		currentLinkPath, err := filepath.EvalSymlinks(fetcherLinkPath)
		if err != nil {
			return err
		}
		if currentLinkPath != linkRelativePath {
			return errors.New("same output but different fetcher")
		}
	}

	// arguments
	argumentsPath := filepath.Join(DownloadsDir, DownloadArgumentsFileName)
	return writeArguments(argumentsPath, dl.Arguments)
}

func GetFetcher(name string) (f Fetcher, err error) {
	path := filepath.Join(FetchersDir, name)

	raw, err := ioutil.ReadFile(path)
	if err != nil {
		return
	}

	args, err := unpackArguments(raw)
	if err != nil {
		return
	}

	fetcher := Fetcher{name, args}

	return fetcher, nil
}

func AddFetcher(fetcher Fetcher) error {
	path := filepath.Join(FetchersDir, fetcher.Name)
	return writeArguments(path, fetcher.Arguments)
}
