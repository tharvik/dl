package internal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
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

func unpackArguments(encoded []byte) ([]string, error) {
	// for empty state file created by flock
	if len(encoded) == 0 {
		return nil, nil
	}

	buf := bytes.NewBuffer(encoded)

	var size uint32
	if err := binary.Read(buf, binary.BigEndian, &size); err != nil {
		return nil, fmt.Errorf("read size: %v", err)
	}

	elems_sizes := make([]uint32, size)
	for i := uint32(0); i < size; i++ {
		if err := binary.Read(buf, binary.BigEndian, &elems_sizes[i]); err != nil {
			return nil, fmt.Errorf("read element %v: %v", i, err)
		}
	}

	elems := make([]string, size)

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
		return fmt.Errorf("truncate: %v", err)
	}

	packed := packArguments(arguments)
	if _, err := file.Write(packed); err != nil {
		return fmt.Errorf("write: %v", err)
	}

	return nil
}

func writeArguments(path string, arguments []string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create: %v", err)
	}
	defer file.Close()

	return writeArgumentsWithFile(file, arguments)
}

func readArguments(path string) ([]string, error) {
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return unpackArguments(raw)
}

type DB struct{ path string }

func NewDB(prefix string) (DB, error) {
	for _, dir := range []string{
		filepath.Join(prefix, ConfigDir),
		filepath.Join(prefix, FetchersDir),
		filepath.Join(prefix, DownloadsDir),
	} {
		if err := os.Mkdir(dir, os.ModePerm); err != nil && !os.IsExist(err) {
			return DB{}, fmt.Errorf("mkdir %v: %v", dir, err)
		}
	}

	return DB{prefix}, nil
}

func (db DB) AddDownload(dl Download) error {
	dlsDir := filepath.Join(db.path, DownloadsDir)

	fileLock := flock.New(dlsDir)
	defer fileLock.Close()

	if err := fileLock.Lock(); err != nil {
		return fmt.Errorf("lock %v: %v", dlsDir, err)
	}
	defer fileLock.Unlock()

	// output
	downloadPath := filepath.Join(dlsDir, dl.Name)
	if err := os.MkdirAll(downloadPath, os.ModePerm); err != nil && !os.IsExist(err) {
		return fmt.Errorf("mkdir all %v: %v", downloadPath, err)
	}

	// fetcher
	fetcherPath := filepath.Join(db.path, FetchersDir, dl.Fetcher.Name)
	fetcherLinkPath := filepath.Join(downloadPath, DownloadFetcherFileName)
	linkRelativePath, err := filepath.Rel(downloadPath, fetcherPath)
	if err != nil {
		return fmt.Errorf("relative path between %v and %v: %v", downloadPath, fetcherPath, err)
	}

	if err = os.Symlink(linkRelativePath, fetcherLinkPath); err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("symlink: %v", err)
		}

		currentLinkRaw := make([]byte, 256)
		size, err := syscall.Readlink(fetcherLinkPath, currentLinkRaw)
		if err != nil {
			return fmt.Errorf("readlink: %v", err)
		}
		if currentLinkContent := string(currentLinkRaw[:size]); currentLinkContent != linkRelativePath {
			return errors.New("same output but different fetcher")
		}
	}

	// arguments
	argumentsPath := filepath.Join(downloadPath, DownloadArgumentsFileName)
	if err := writeArguments(argumentsPath, dl.Arguments); err != nil {
		return fmt.Errorf("write arguments: %v", err)
	}

	return nil
}

func (db DB) GetDownloads() ([]Download, error) {
	dlsDir := filepath.Join(db.path, DownloadsDir)

	fileLock := flock.New(dlsDir)
	defer fileLock.Close()

	if err := fileLock.RLock(); err != nil {
		return nil, fmt.Errorf("lock downloads dir: %v", err)
	}

	getDownload := func(name string) (*Download, error) {
		dlDir := filepath.Join(dlsDir, name)
		dlFetcherPath := filepath.Join(dlDir, DownloadFetcherFileName)

		dlArgsPath := filepath.Join(dlDir, DownloadArgumentsFileName)
		dlArgs, err := readArguments(dlArgsPath)
		if err != nil {
			return nil, fmt.Errorf("read arguments: %w", err)
		}

		dlFetcherRaw := make([]byte, 256)
		size, err := syscall.Readlink(dlFetcherPath, dlFetcherRaw)
		if err != nil {
			return nil, fmt.Errorf("read link: %v", err)
		}
		dlFetcherName := filepath.Base(string(dlFetcherRaw[:size]))

		fetcher, err := db.GetFetcher(dlFetcherName)
		if err != nil {
			return nil, fmt.Errorf("get fetcher: %v", err)
		}

		return &Download{name, fetcher, dlArgs}, nil
	}

	var getDownloadsInDlsDir func(string) ([]Download, error)
	getDownloadsInDlsDir = func(name string) ([]Download, error) {
		dlDir := filepath.Join(dlsDir, name)
		dirs, err := ioutil.ReadDir(dlDir)
		if err != nil {
			return nil, fmt.Errorf("read dir: %v", err)
		}

		dls := make([]Download, 0, len(dirs))
		for _, dir := range dirs {
			dlName := filepath.Join(name, dir.Name())

			dl, err := getDownload(dlName)
			if errors.Is(err, os.ErrNotExist) {
				subs, err := getDownloadsInDlsDir(dlName)
				if err != nil {
					return nil, err
				}

				dls = append(dls, subs...)
			} else if err != nil {
				return nil, fmt.Errorf("get download: %v", err)
			} else {
				dls = append(dls, *dl)
			}
		}

		return dls, nil
	}

	dls, err := getDownloadsInDlsDir("")
	if err != nil {
		return nil, fmt.Errorf("get downloads in downloads dir: %v", err)
	}

	return dls, nil
}

func (db DB) DelDownload(dl Download) error {
	dlsDir := filepath.Join(db.path, DownloadsDir)

	fileLock := flock.New(dlsDir)
	defer fileLock.Close()

	if err := fileLock.Lock(); err != nil {
		return fmt.Errorf("lock downloads dir: %v", err)
	}

	dir := filepath.Join(db.path, DownloadsDir, dl.Name)
	for _, path := range []string{
		filepath.Join(dir, DownloadArgumentsFileName),
		filepath.Join(dir, DownloadFetcherFileName),
		dir,
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %v: %v", path, err)
		}
	}

	return nil
}

func (db DB) GetFetcher(name string) (Fetcher, error) {
	ftsDir := filepath.Join(db.path, FetchersDir)

	fileLock := flock.New(ftsDir)
	defer fileLock.Close()

	if err := fileLock.RLock(); err != nil {
		return Fetcher{}, fmt.Errorf("lock fetcher dir: %v", err)
	}

	path := filepath.Join(db.path, FetchersDir, name)
	args, err := readArguments(path)
	if err != nil {
		return Fetcher{}, fmt.Errorf("read arguments: %v", err)
	}

	return Fetcher{name, args}, nil
}

func (db DB) AddFetcher(fetcher Fetcher) error {
	ftsDir := filepath.Join(db.path, FetchersDir)
	path := filepath.Join(ftsDir, fetcher.Name)

	fileLock := flock.New(ftsDir)
	defer fileLock.Close()

	if err := fileLock.Lock(); err != nil {
		return fmt.Errorf("lock fetcher dir: %v", err)
	}

	if err := writeArguments(path, fetcher.Arguments); err != nil {
		return fmt.Errorf("write arguments: %v", err)
	}

	return nil
}

func (db DB) GetState() ([]string, error) {
	path := filepath.Join(db.path, StateFile)

	fileLock := flock.New(path)
	defer fileLock.Close()

	if err := fileLock.RLock(); err != nil {
		return nil, fmt.Errorf("lock state file: %v", err)
	}

	args, err := readArguments(path)
	if err != nil {
		return nil, fmt.Errorf("read arguments: %v", err)
	}

	return args, nil
}

func (db DB) SetState(args []string) error {
	path := filepath.Join(db.path, StateFile)

	fileLock := flock.New(path)
	defer fileLock.Close()

	if err := fileLock.Lock(); err != nil {
		return fmt.Errorf("lock state file: %v", err)
	}

	if err := writeArguments(path, args); err != nil {
		return fmt.Errorf("write arguments: %v", err)
	}

	return nil
}
