//  Package qcow implements qcow2 images support for clair
package qcow

import (
	"io"
	"os"
	"os/exec"
	"io/ioutil"
	"sync/atomic"
	"path/filepath"
	"strconv"
	"bufio"
	"bytes"
	"strings"
	"math/rand"
	"fmt"


	"github.com/coreos/clair/ext/imagefmt"
	"github.com/coreos/clair/pkg/tarutil"
)


var scanCount int32

type ImgCount struct {
	Imgcount string
}

type format struct{}

func init() {
	imagefmt.RegisterExtractor("qcow", &format{})
}


func writeImg(img io.ReadCloser, path string) (error) {
	writer, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err = io.Copy(writer, img); err != nil {
		return err
	}
	return nil
}

func getPartsOffsets(path string) ([]string, error) {
	cmd := exec.Command("/bin/sh", "-c", "fdisk -l " + path)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("Could not get qcow partitions: %s", err.Error())
	}
	var parts = make([]string, 0)
	scanner := bufio.NewScanner(bytes.NewBuffer(output))
	for scanner.Scan() {
		// Linux filesystem -> gpt partition
		if strings.Contains(scanner.Text(), "Linux filesystem") {
			parts = append(parts, strings.Fields(scanner.Text())[1])
		// Linux -> msdos partition
		} else if strings.Contains(scanner.Text(), "Linux") {
			parts = append(parts, strings.Fields(scanner.Text())[2])
		}
	}
	return parts, nil
}

func mountImg(layerReader io.ReadCloser) (string, error) {
	curScanCount := atomic.LoadInt32(&scanCount)
	if curScanCount >= 5 {
		return "", fmt.Errorf("Too many qcow scans are running")
	}
	atomic.AddInt32(&scanCount, 1)
	curId := string(strconv.Itoa(rand.Int()))
	if err := os.MkdirAll("/mnt/qcow" + curId, 0700); err != nil {
		return "", err
	}
	if err := os.MkdirAll("/mnt/parts" + curId, 0700); err != nil {
		return curId, err
	}
	if err := writeImg(layerReader, "/tmp/img" + curId); err != nil {
		return curId, err
	}
	cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf("qcowmount -X allow_other /tmp/img%s /mnt/qcow%s", curId, curId))
	if err := cmd.Run(); err != nil {
		return curId, fmt.Errorf("Could not mount qcow image: %s", err)
	}
	offsets, err := getPartsOffsets(filepath.Join("/mnt/qcow" + curId, "qcow1"))
	if err != nil {
		return curId, err
	}
	for _, offset := range(offsets) {
		if err := os.MkdirAll(filepath.Join("/mnt/parts" + curId, offset), 0700); err != nil {
			return curId, err
		}
		strcmd := fmt.Sprintf("mount -o offset=$((512*%s)),ro %s %s", offset, filepath.Join("/mnt/qcow" + curId, "qcow1"), filepath.Join("/mnt/parts" + curId, offset))
		cmd := exec.Command("/bin/sh", "-c",  strcmd)
		if err := cmd.Run(); err != nil {
			return curId, fmt.Errorf("Could not mount qcow partitions: %s", err)
		}
	}
	return curId, nil
}

func umountImg(curId string) (error) {
	defer atomic.AddInt32(&scanCount, -1)
	umountcmd := fmt.Sprintf("umount /mnt/parts%s/*", curId)
	cmd := exec.Command("/bin/sh", "-c", umountcmd)
	if err := cmd.Run(); err != nil {
		return err
	}
	umountcmd = fmt.Sprintf("fusermount -u /mnt/qcow%s", curId)
	cmd = exec.Command("/bin/sh", "-c", umountcmd)
	if err := cmd.Run(); err != nil {
		return err
	}
	os.RemoveAll("/mnt/parts" + curId)
	os.RemoveAll("/mnt/qcow" + curId)
	os.RemoveAll("/tmp/img" + curId)
	return nil
}

func (f format) ExtractFiles(layerReader io.ReadCloser, toExtract []string) (tarutil.FilesMap, error) {
	curCount, err := mountImg(layerReader)
	defer umountImg(curCount)
	if err != nil {
		return nil, err
	}
	files, err := ioutil.ReadDir("/mnt/parts" + curCount)
	if err != nil {
		return nil, err
	}
	var extracted tarutil.FilesMap = make(tarutil.FilesMap)
	for _, f := range files {
		for _, tofind := range(toExtract) {
			path := filepath.Join("/mnt/parts" + curCount, f.Name(), tofind)
			if _, err := os.Stat(path); err == nil {
				if data, err := ioutil.ReadFile(path); err != nil {
					continue
				} else {
					extracted[tofind] = data
				}
			}
		}
	}
	return extracted, nil
}
