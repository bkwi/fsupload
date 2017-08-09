package main

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strconv"
)

const workersCount int = 3
const partSize int = 1024 * 1024 * 5

// const partSize int = 20 * 1024

const multipartStartURL = "https://upload.filestackapi.com/multipart/start"
const multipartUploadURL = "https://upload.filestackapi.com/multipart/upload"
const multipartCompleteURL = "https://upload.filestackapi.com/multipart/complete"

// UploadJob channel bla bla
type UploadJob struct {
	// data  []byte
	offset        int
	size          int
	num           int
	sresp         startResponse
	apikey        string
	storeLocation string
}

// Response bla bla
type Response struct {
	success   bool
	part      int
	bytesSent int
	etag      string
}

// UploadData a
type UploadData struct {
	reader   io.ReaderAt
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

type filelinkResponse struct {
	URL string `json:"url"`
}

type uploadInitResponse struct {
	URL     string
	Headers map[string]string
}

type startRequestData struct {
	APIKey        string `json:"apikey"`
	StoreLocation string `json:"store_location"`
	Mimetype      string `json:"mimetype"`
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
}

type uploadPostRequestData struct {
	APIKey        string `json:"apikey"`
	Part          int
	StoreLocation string `json:"store_location"`
	Size          int64  `json:"size"`
	MD5           string
	URI           string
	Region        string
	UploadID      string
}

func uploadChunk(f io.ReaderAt, size int64, uc chan UploadJob, rc chan Response) {
	reader := io.NewSectionReader(f, 0, size)
	for job := range uc {
		buff := make([]byte, job.size)
		reader.Seek(int64(job.offset), 0)
		_, err := reader.Read(buff)
		if err != nil {
			panic(err)
		}

		var b bytes.Buffer
		x := multipart.NewWriter(&b)
		x.WriteField("apikey", job.apikey)
		x.WriteField("size", strconv.Itoa(int(job.size)))
		x.WriteField("store_location", job.storeLocation)
		x.WriteField("part", strconv.Itoa(int(job.num)))
		x.WriteField("uri", job.sresp.URI)
		x.WriteField("region", job.sresp.Region)
		x.WriteField("upload_id", job.sresp.UploadID)

		h := md5.New()
		h.Write(buff)
		x.WriteField("md5", base64.StdEncoding.EncodeToString(h.Sum(nil)))

		err = x.Close()
		req, err := http.NewRequest("POST", multipartUploadURL, &b)
		req.Header.Set("Content-Type", x.FormDataContentType())

		resp, err := (&http.Client{}).Do(req)
		defer resp.Body.Close()

		if err != nil {
			panic(err)
		}

		var uiresp uploadInitResponse
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&uiresp); err != nil {
			panic(err)
		}

		req, err = http.NewRequest("PUT", uiresp.URL, bytes.NewReader(buff))
		for k, v := range uiresp.Headers {
			req.Header.Set(k, v)
		}

		s3resp, err := (&http.Client{}).Do(req)
		defer s3resp.Body.Close()
		if err != nil {
			panic(err)
		}

		rc <- Response{true, job.num, job.size, s3resp.Header.Get("ETag")}
	}
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
	req, err := http.NewRequest("POST", multipartStartURL, &b)
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

func multipartComplete(content UploadData, settings UploadSettings, sresp startResponse, etags string) string {
	var b bytes.Buffer
	x := multipart.NewWriter(&b)
	x.WriteField("apikey", settings.apikey)
	x.WriteField("uri", sresp.URI)
	x.WriteField("region", sresp.Region)
	x.WriteField("upload_id", sresp.UploadID)
	x.WriteField("filename", content.filename)
	x.WriteField("size", strconv.Itoa(int(content.size)))
	x.WriteField("mimetype", content.mimetype)
	x.WriteField("parts", etags)
	x.WriteField("store_location", settings.storeLocation)

	err := x.Close()
	req, err := http.NewRequest("POST", multipartCompleteURL, &b)
	req.Header.Set("Content-Type", x.FormDataContentType())

	resp, err := (&http.Client{}).Do(req)
	defer resp.Body.Close()

	if err != nil {
		panic(err)
	}

	var flink filelinkResponse
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&flink); err != nil {
		panic(err)
	}

	return flink.URL
}

func upload(content UploadData, settings UploadSettings) {

	sresp := multipartStart(content, settings)
	fmt.Println(sresp)

	uc := make(chan UploadJob, workersCount) // upload channel
	rc := make(chan Response, workersCount)  // response channel

	defer close(uc)
	defer close(rc)

	// start upload goroutines
	for i := 0; i < workersCount; i++ {
		go uploadChunk(content.reader, content.size, uc, rc)
	}

	go func(r io.ReaderAt, uc chan UploadJob, settings UploadSettings) {
		part := 1
		for i := 0; i < int(content.size); i += partSize {
			end := int(math.Min(float64(i+partSize), float64(content.size)))
			uc <- UploadJob{i, end - i, part, sresp, settings.apikey, settings.storeLocation}
			part++
		}
	}(content.reader, uc, settings)

	var etags string
	bytesLeft := content.size
	for bytesLeft > 0 {
		resp := <-rc
		if etags != "" {
			etags += ";"
		}
		etags += strconv.Itoa(resp.part) + ":" + resp.etag
		bytesLeft -= int64(resp.bytesSent)
	}

	final := multipartComplete(content, settings, sresp, etags)
	fmt.Println("done", final)

}

func main() {
	filepath := "test_files/2.jpg"
	f, err := os.Open(filepath)

	if err != nil {
		panic(err)
	}

	stat, err := f.Stat()
	mimetype := mime.TypeByExtension(path.Ext(filepath))
	content := UploadData{f, stat.Name(), stat.Size(), mimetype}
	settings := UploadSettings{"AZ25y30ZiRnG7ahX6iMYLz", "s3"}
	upload(content, settings)
}
