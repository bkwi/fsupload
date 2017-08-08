package main

import (
	"fmt"
	"io"
	"math"
	"mime"
	"os"
	"path"
	"time"
)

const workersCount int = 5
const partSize int = 20 * 1024

// UploadJob channel bla bla
type UploadJob struct {
	data []byte
	num  int
}

// Response bla bla
type Response struct {
	workerID int
}

func uploadChunk(uc chan UploadJob, rc chan Response, workerID int) {
	for job := range uc {
		// fmt.Println("Sleep")
		time.Sleep(10 * time.Millisecond)
		// time.Sleep(time.Second)
		// fmt.Println("Awake")
		fmt.Println(job.num, "got", workerID)
		rc <- Response{workerID}
	}
}

// UploadData a
type UploadData struct {
	reader   io.Reader
	filename string
	size     int64
	mimetype string
}

// UploadSettings comments
type UploadSettings struct {
	apikey          string
	storageLocation string
}

type startResponse struct {
	uri      string
	region   string
	uploadID string
}

func multipartStart(content UploadData, settings UploadSettings) startResponse {
	return startResponse{}
}

func upload(content UploadData, settings UploadSettings) {

	sr := multipartStart(content, settings)
	fmt.Println(sr)

	partsNumber := int(math.Ceil(float64(content.size) / float64(partSize)))

	uc := make(chan UploadJob, workersCount) // upload channel
	rc := make(chan Response, workersCount)  // response channel

	defer close(uc)
	defer close(rc)

	parts := make([][]byte, workersCount)
	partsInProgress := 0

	for i := 0; i < workersCount; i++ {
		parts[i] = make([]byte, partSize)
		bt, err := content.reader.Read(parts[i])

		if err == io.EOF {
			break
		} else if err != nil {
			panic(err)
		}
		partsInProgress++

		uc <- UploadJob{parts[i][:bt], i}
		go uploadChunk(uc, rc, i)

	}

	partsDone := 0
	for resp := range rc {
		fmt.Println(resp)

		partsDone++

		if partsInProgress < partsNumber {
			bt, err := content.reader.Read(parts[resp.workerID])
			if err == io.EOF {
				fmt.Println("OH NO")
				break
			} else if err != nil {
				panic(err)
			}

			uc <- UploadJob{parts[resp.workerID][:bt], partsInProgress}
			partsInProgress++
		}

		if partsDone >= partsNumber {
			break
		}

	}

}

func main() {
	filepath := "/Users/bartosz/Desktop/test_files/ball.png"
	f, err := os.Open(filepath)

	if err != nil {
		panic(err)
	}

	stat, err := f.Stat()
	mimetype := mime.TypeByExtension(path.Ext(filepath))
	content := UploadData{f, stat.Name(), stat.Size(), mimetype}
	fmt.Println("CONTENT", content)
	settings := UploadSettings{"Axz", "s3"}
	upload(content, settings)
	// fmt.Println(mime.TypeByExtension(".zip"))
}
