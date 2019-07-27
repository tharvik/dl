package lib

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

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
	if err = binary.Read(buf, binary.BigEndian, &size); err != nil {
		return
	}

	elems_sizes := make([]uint32, size)
	for i := uint32(0); i < size; i++ {
		if err = binary.Read(buf, binary.BigEndian, &elems_sizes[i]); err != nil {
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

func writeArgumentsWithFile(file *os.File, arguments []string) error {
	if err := file.Truncate(0); err != nil {
		return err
	}

	packed := packArguments(arguments)
	if _, err := file.Write(packed); err != nil {
		return err
	}

	return nil
}

func writeArguments(path string, arguments []string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return writeArgumentsWithFile(file, arguments)
}

func readArguments(path string) ([]string, error) {
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return unpackArguments(raw)
}

type DB struct {
	path string
}

func NewDB(prefix string) (db DB, err error) {
	for _, dir := range []string{
		filepath.Join(prefix, ConfigDir),
		filepath.Join(prefix, FetchersDir),
		filepath.Join(prefix, DownloadsDir),
	} {
		if err = os.Mkdir(dir, os.ModePerm); err != nil && !os.IsExist(err) {
			return
		}
	}

	stateFile, err := os.OpenFile(filepath.Join(prefix, StateFile), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if !os.IsExist(err) {
			return
		}
	} else {
		err = writeArgumentsWithFile(stateFile, []string{})
		if err != nil {
			return
		}
	}

	return DB{prefix}, nil
}

func (db DB) AddDownload(dl Download) error {
	dlsDir := filepath.Join(db.path, DownloadsDir)

	fileLock := flock.New(dlsDir)
	defer fileLock.Close()

	if err := fileLock.Lock(); err != nil {
		return err
	}

	// output
	downloadPath := filepath.Join(dlsDir, dl.Name)
	if err := os.MkdirAll(downloadPath, os.ModePerm); err != nil && !os.IsExist(err) {
		return err
	}

	// fetcher
	fetcherPath := filepath.Join(db.path, FetchersDir, dl.Fetcher.Name)
	fetcherLinkPath := filepath.Join(downloadPath, DownloadFetcherFileName)
	linkRelativePath, err := filepath.Rel(downloadPath, fetcherPath)
	if err != nil {
		return err
	}

	if err = os.Symlink(linkRelativePath, fetcherLinkPath); err != nil {
		if !os.IsExist(err) {
			return err
		}

		currentLinkRaw := make([]byte, 256)
		size, err := syscall.Readlink(fetcherLinkPath, currentLinkRaw)
		if err != nil {
			return err
		}
		if currentLinkContent := string(currentLinkRaw[:size]); currentLinkContent != linkRelativePath {
			return errors.New("same output but different fetcher")
		}
	}

	// arguments
	argumentsPath := filepath.Join(downloadPath, DownloadArgumentsFileName)
	return writeArguments(argumentsPath, dl.Arguments)
}

func (db DB) GetDownloads() ([]Download, error) {
	dlsDir := filepath.Join(db.path, DownloadsDir)

	fileLock := flock.New(dlsDir)
	defer fileLock.Close()

	if err := fileLock.RLock(); err != nil {
		return nil, err
	}

	getDownloadNotFoundError := errors.New("unable to find any file to parse")
	getDownload := func(name string) (*Download, error) {
		dlDir := filepath.Join(dlsDir, name)
		dlFetcherPath := filepath.Join(dlDir, DownloadFetcherFileName)
		dlFetcherFile, errOpenFetcher := os.Open(dlFetcherPath)
		if errOpenFetcher != nil && !os.IsNotExist(errOpenFetcher) {
			return nil, errOpenFetcher
		}

		dlArgsPath := filepath.Join(dlDir, DownloadArgumentsFileName)
		dlArgs, errOpenArgs := readArguments(dlArgsPath)
		if errOpenArgs != nil && !os.IsNotExist(errOpenArgs) {
			return nil, errOpenArgs
		}

		if errOpenFetcher != nil && errOpenArgs != nil {
			return nil, getDownloadNotFoundError
		}

		defer dlFetcherFile.Close()

		dlFetcherRaw := make([]byte, 256)
		size, err := syscall.Readlink(dlFetcherPath, dlFetcherRaw)
		if err != nil {
			return nil, err
		}
		dlFetcherName := filepath.Base(string(dlFetcherRaw[:size]))

		fetcher, err := db.GetFetcher(dlFetcherName)
		if err != nil {
			return nil, err
		}

		return &Download{name, fetcher, dlArgs}, nil
	}

	var getDownloadsInDlsDir func(string) ([]Download, error)
	getDownloadsInDlsDir = func(name string) ([]Download, error) {
		dlDir := filepath.Join(dlsDir, name)
		dirs, err := ioutil.ReadDir(dlDir)
		if err != nil {
			return nil, err
		}

		dls := make([]Download, 0, len(dirs))
		for _, dir := range dirs {
			dlName := filepath.Join(name, dir.Name())

			dl, err := getDownload(dlName)
			if err == getDownloadNotFoundError {
				subs, err := getDownloadsInDlsDir(dlName)
				if err != nil {
					return nil, err
				}

				dls = append(dls, subs...)
			} else if err != nil {
				return nil, err
			} else {
				dls = append(dls, *dl)
			}
		}

		return dls, nil
	}

	return getDownloadsInDlsDir("")
}

func (db DB) DelDownload(dl Download) error {
	dlsDir := filepath.Join(db.path, DownloadsDir)

	fileLock := flock.New(dlsDir)
	defer fileLock.Close()

	if err := fileLock.Lock(); err != nil {
		return err
	}

	dir := filepath.Join(db.path, DownloadsDir, dl.Name)
	for _, path := range []string{
		filepath.Join(dir, DownloadArgumentsFileName),
		filepath.Join(dir, DownloadFetcherFileName),
		dir,
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

func (db DB) GetFetcher(name string) (f Fetcher, err error) {
	ftsDir := filepath.Join(db.path, FetchersDir)

	fileLock := flock.New(ftsDir)
	defer fileLock.Close()

	if err = fileLock.RLock(); err != nil {
		return
	}

	path := filepath.Join(db.path, FetchersDir, name)
	args, err := readArguments(path)
	return Fetcher{name, args}, nil
}

func (db DB) AddFetcher(fetcher Fetcher) error {
	ftsDir := filepath.Join(db.path, FetchersDir)
	path := filepath.Join(ftsDir, fetcher.Name)

	fileLock := flock.New(ftsDir)
	defer fileLock.Close()

	if err := fileLock.Lock(); err != nil {
		return err
	}

	return writeArguments(path, fetcher.Arguments)
}

func (db DB) GetState() ([]string, error) {
	path := filepath.Join(db.path, StateFile)

	fileLock := flock.New(path)
	defer fileLock.Close()

	if err := fileLock.RLock(); err != nil {
		return []string{}, err
	}

	args, err := readArguments(path)
	return args, err
}

func (db DB) SetState(args []string) error {
	path := filepath.Join(db.path, StateFile)

	fileLock := flock.New(path)
	defer fileLock.Close()

	if err := fileLock.Lock(); err != nil {
		return err
	}

	return writeArguments(path, args)
}
