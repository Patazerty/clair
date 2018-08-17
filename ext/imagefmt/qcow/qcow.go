// Package qcow implements qcow2 images support for clair
package qcow

import (
	"io"
	"os"
	"os/exec"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"bufio"
	"bytes"
	"strings"
	"math/rand"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/coreos/clair/ext/imagefmt"
        "github.com/coreos/clair/pkg/tarutil"
)

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

func getParts(path string) (map[string]string, error) {
	cmd := exec.Command("/bin/sh", "-c", "echo 'print list' | parted " + path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("Could not get qcow partitions: %s: %s", err.Error(), string(output))
	}
	log.WithFields(log.Fields{"output": string(output), "block_file": path}).Debug("Parted output")
	parts := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewBuffer(output))
	for scanner.Scan() {
		// Msdos partition table
		if strings.Contains(scanner.Text(), "primary") || strings.Contains(scanner.Text(), "extended"){
			partNb := string(strings.Fields(scanner.Text())[0])
			partType := string(strings.Fields(scanner.Text())[5])
			parts[partNb] = partType
			log.WithFields(log.Fields{"type": partType,
				"block_file": path, "part": partNb}).Debug("Found partition")
		// gpt partition table
		} else if strings.Contains(scanner.Text(), "xfs") || strings.Contains(scanner.Text(), "ext4")  {
			partNb := string(strings.Fields(scanner.Text())[0])
			partType := string(strings.Fields(scanner.Text())[4])
			parts[partNb] = partType
			log.WithFields(log.Fields{"type": partType,
				"block_file": path, "part": partNb}).Debug("Found partition")
		}
	}
	log.WithFields(log.Fields{"parts": parts}).Debug("Paritions found")
	return parts, nil
}

func mountImg(layerReader io.ReadCloser) (string, error) {
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
	strcmd := fmt.Sprintf("qcowmount -X allow_other /tmp/img%s /mnt/qcow%s", curId, curId)
	log.WithFields(log.Fields{"command": strcmd}).Debug("System exec")
	cmd := exec.Command("/bin/sh", "-c", strcmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		return curId, fmt.Errorf("Could not mount qcow image: %s: %s", err, string(output))
	}
	blockFile := filepath.Join("/mnt/qcow" + curId, "qcow1")
	parts, err := getParts(blockFile)
	if err != nil {
		return curId, err
	}
	for partNb, partType := range(parts) {
		mountPath := filepath.Join("/mnt/parts" + curId, partNb)
		if err := os.MkdirAll(mountPath, 0700); err != nil {
			return curId, err
		}
		strcmd := fmt.Sprintf("lklfuse -o ro -o type=%s -o part=%s %s %s", partType, partNb, blockFile, mountPath)
		log.WithFields(log.Fields{"command": strcmd}).Debug("System exec")
		cmd := exec.Command("/bin/sh", "-c",  strcmd)
		if output, err := cmd.CombinedOutput(); err != nil {
			return curId, fmt.Errorf("Could not mount qcow partitions: %s: %s", err, string(output))
		}
	}
	return curId, nil
}

func umountImg(curId string) (error) {
	files, err := ioutil.ReadDir("/mnt/parts" + curId)
	if err != nil {
		return fmt.Errorf("Could not list directory: %s", err)
	}
	for _, file := range(files) {
		umountcmd := fmt.Sprintf("umount /mnt/parts%s/%s", curId, file.Name())
		cmd := exec.Command("/bin/sh", "-c", umountcmd)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("Could not umount part: %s: %s", err, string(output))
		}
	}
	umountcmd := fmt.Sprintf("fusermount -u /mnt/qcow%s", curId)
	cmd := exec.Command("/bin/sh", "-c", umountcmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Could not umount block file: %s: %s", err, string(output))
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
