package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
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
	apikey        string
	storeLocation string
}

type startResponse struct {
	uri      string
	region   string
	uploadID string
}

type startRequestData struct {
	ApiKey        string `json:"apikey"`
	StoreLocation string `json:"store_location"`
	Mimetype      string `json:"mimetype"`
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
}

func multipartStart(content UploadData, settings UploadSettings) startResponse {

	reqParams := startRequestData{
		ApiKey:        settings.apikey,
		StoreLocation: settings.storeLocation,
		Mimetype:      content.mimetype,
		Filename:      content.filename,
		Size:          content.size,
	}
	jsonData, _ := json.Marshal(reqParams)
	var b bytes.Buffer
	x := multipart.NewWriter(&b)
	x.CreateFormField("apikey")
	err := x.Close()
	fmt.Println("HHHHHERE:", jsonData)
	req, err := http.NewRequest("POST", "https://requestb.in/z9spndz9", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=lol")
	fmt.Println(req, err)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	fmt.Println(resp, err)
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
	filepath := "test_files/1.jpg"
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
