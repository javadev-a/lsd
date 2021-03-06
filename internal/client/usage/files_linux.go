package usage

import (
	"fmt"
	"io"
	"os"

	"badoo/_packages/log"
	"github.com/badoo/lsd/internal/client/files"
)

// get unused files with inodes specified in filter map
func getFreeFiles(filter map[uint64]bool) (map[uint64]bool, error) {

	result := make(map[uint64]bool)
	for ino, _ := range filter {
		result[ino] = true
	}

	ignorePid := fmt.Sprint(os.Getpid())

	dh, err := os.Open("/proc")
	if err != nil {
		return nil, err
	}
	defer dh.Close()

	for {
		fis, err := dh.Readdir(100)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("could not read directory names: %v", err)
		}
		for _, fi := range fis {
			if fi.Name() == ignorePid {
				log.Debugf("Ignoring self pid = %s", ignorePid)
				continue
			}
			if fi.IsDir() {
				deleteUsedFiles("/proc/"+fi.Name()+"/fd", result)
			}
		}
	}
	return result, nil
}

func deleteUsedFiles(procDir string, result map[uint64]bool) {
	dh, err := os.Open(procDir)
	if err != nil {
		log.Debugf("Could not open %s: %s", procDir, err.Error())
		return
	}
	defer dh.Close()

	for {
		name, err := dh.Readdirnames(100)
		if err == nil {
			for _, name := range name {
				path := procDir + "/" + name
				st, err := files.Stat(path)
				if err == nil {
					delete(result, st.Inode())
				} else if !os.IsNotExist(err) {
					log.Debugf("Could not stat %s: %s", path, err.Error())
				}
			}
		} else if err == io.EOF {
			break
		} else {
			log.Debugf("Could not read directory names: " + err.Error())
			break
		}
	}
}
