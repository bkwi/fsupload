package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"
)

const workersCount int = 5
const partSize int = 1024 * 1024

// const partSize int = 20 * 1024

// MultipartStartURL bla
const MultipartStartURL = "https://upload.filestackapi.com/multipart/start"

// UploadJob channel bla bla
type UploadJob struct {
	// data  []byte
	bytes [partSize]byte
	num   int
}

// Response bla bla
type Response struct {
	success   bool
	bytesSent int
}

func uploadChunk(uc chan UploadJob, rc chan Response) {
	for job := range uc {
		fmt.Println("Got job", job.num)
		time.Sleep(10 * time.Millisecond)
		// time.Sleep(time.Second)
		// fmt.Println("Awake")
		// fmt.Println(job.num, "got", workerID)
		rc <- Response{}
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
	URI      string `json:"uri"`
	Region   string `json:"region"`
	UploadID string `json:"upload_id"`
}

type startRequestData struct {
	APIKey        string `json:"apikey"`
	StoreLocation string `json:"store_location"`
	Mimetype      string `json:"mimetype"`
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
}

func multipartStart(content UploadData, settings UploadSettings) startResponse {

	reqParams := startRequestData{
		APIKey:        settings.apikey,
		StoreLocation: settings.storeLocation,
		Mimetype:      content.mimetype,
		Filename:      content.filename,
		Size:          content.size,
	}

	size := strconv.Itoa(int(reqParams.Size))

	var b bytes.Buffer
	x := multipart.NewWriter(&b)
	x.WriteField("apikey", reqParams.APIKey)
	x.WriteField("size", size)
	x.WriteField("filename", reqParams.Filename)
	x.WriteField("mimetype", reqParams.Mimetype)
	x.WriteField("store_location", reqParams.StoreLocation)

	err := x.Close()
	req, err := http.NewRequest("POST", MultipartStartURL, &b)
	req.Header.Set("Content-Type", x.FormDataContentType())

	resp, err := (&http.Client{}).Do(req)
	defer resp.Body.Close()

	if err != nil {
		panic(err)
	}

	var sresp startResponse
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&sresp); err != nil {
		panic(err)
	}

	return sresp
}

func upload(content UploadData, settings UploadSettings) {

	sresp := multipartStart(content, settings)
	fmt.Println(sresp)

	// partsNumber := int(math.Ceil(float64(content.size) / float64(partSize)))

	uc := make(chan UploadJob, workersCount) // upload channel
	rc := make(chan Response, workersCount)  // response channel

	defer close(uc)
	defer close(rc)

	// start upload goroutines
	for i := 0; i < workersCount; i++ {
		go uploadChunk(uc, rc)
	}

	go func(r io.Reader, uc chan UploadJob) {
		part := 1
		job := UploadJob{}
		buff := make([]byte, partSize)
		for {
			b, err := r.Read(buff)
			fmt.Println("Read bytes", b)
			if err == io.EOF {
				break
			} else if err != nil {
				panic(err)
			}
			copy(job.bytes[:], buff[0:b])
			job.num = part
			uc <- job
			part++
		}
	}(content.reader, uc)

	for {
		<-rc
	}

	// parts := make([][]byte, workersCount)
	// partsInProgress := 0

	// for i := 0; i < workersCount; i++ {
	// 	parts[i] = make([]byte, partSize)
	// 	bt, err := content.reader.Read(parts[i])

	// 	if err == io.EOF {
	// 		break
	// 	} else if err != nil {
	// 		panic(err)
	// 	}
	// 	partsInProgress++

	// 	uc <- UploadJob{parts[i][:bt], i}
	// 	go uploadChunk(uc, rc, i)

	// }

	// partsDone := 0
	// for resp := range rc {
	// 	partsDone++

	// 	if partsInProgress < partsNumber {
	// 		bt, err := content.reader.Read(parts[resp.workerID])
	// 		if err == io.EOF {
	// 			fmt.Println("OH NO")
	// 			break
	// 		} else if err != nil {
	// 			panic(err)
	// 		}

	// 		uc <- UploadJob{parts[resp.workerID][:bt], partsInProgress}
	// 		partsInProgress++
	// 	}

	// 	if partsDone >= partsNumber {
	// 		break
	// 	}

	// }

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
	settings := UploadSettings{"AZ25y30ZiRnG7ahX6iMYLz", "s3"}
	upload(content, settings)
	// fmt.Println(mime.TypeByExtension(".zip"))
}
