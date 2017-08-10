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

func reqMake(client http.Client, method string, url string, form map[string]string, str interface{}) error {
	var b bytes.Buffer

	x := multipart.NewWriter(&b)
	for k, v := range form {
		x.WriteField(k, v)
	}

	err := x.Close()
	req, err := http.NewRequest(method, url, &b)
	req.Header.Set("Content-Type", x.FormDataContentType())

	resp, err := (&client).Do(req)
	defer resp.Body.Close()

	if err != nil {
		return err
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(str); err != nil {
		return err
	}

	return nil
}

func uploadChunk(f io.ReaderAt, size int64, uc chan UploadJob, rc chan Response) {
	reader := io.NewSectionReader(f, 0, size)
	httpClient := http.Client{}
	for job := range uc {
		buff := make([]byte, job.size)
		reader.Seek(int64(job.offset), 0)
		_, err := reader.Read(buff)
		if err != nil {
			panic(err)
		}

		h := md5.New()
		h.Write(buff)

		form := map[string]string{
			"apikey":          job.apikey,
			"size":            strconv.Itoa(int(job.size)),
			"store_locatgion": job.storeLocation,
			"part":            strconv.Itoa(int(job.num)),
			"uri":             job.sresp.URI,
			"region":          job.sresp.Region,
			"upload_id":       job.sresp.UploadID,
			"md5":             base64.StdEncoding.EncodeToString(h.Sum(nil)),
		}

		var uiresp uploadInitResponse
		err = reqMake(httpClient, "POST", multipartUploadURL, form, &uiresp)
		if err != nil {
			panic(err)
		}

		req, err := http.NewRequest("PUT", uiresp.URL, bytes.NewReader(buff))
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

	form := map[string]string{
		"apikey":         settings.apikey,
		"size":           strconv.Itoa(int(content.size)),
		"filename":       content.filename,
		"mimetype":       content.mimetype,
		"store_location": settings.storeLocation,
	}

	var sresp startResponse
	httpClient := http.Client{}

	err := reqMake(httpClient, "POST", multipartStartURL, form, &sresp)
	if err != nil {
		panic(err)
	}

	return sresp
}

func multipartComplete(content UploadData, settings UploadSettings, sresp startResponse, etags string) string {
	form := map[string]string{
		"apikey":         settings.apikey,
		"uri":            sresp.URI,
		"region":         sresp.Region,
		"upload_id":      sresp.UploadID,
		"filename":       content.filename,
		"size":           strconv.Itoa(int(content.size)),
		"mimetype":       content.mimetype,
		"parts":          etags,
		"store_location": settings.storeLocation,
	}

	var flink filelinkResponse
	httpClient := http.Client{}

	err := reqMake(httpClient, "POST", multipartCompleteURL, form, &flink)
	if err != nil {
		panic(err)
	}

	return flink.URL
}

func UploadMultipart(content UploadData, settings UploadSettings) string {

	sresp := multipartStart(content, settings)

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
		etags += ";" + strconv.Itoa(resp.part) + ":" + resp.etag
		bytesLeft -= int64(resp.bytesSent)
	}

	return multipartComplete(content, settings, sresp, etags[1:])
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
	url := UploadMultipart(content, settings)

	fmt.Println(url)
}
