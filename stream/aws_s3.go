package stream

import (
	"os"

	"bytes"
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/abstractpaper/swissarmy"
	log "github.com/sirupsen/logrus"
)

type S3 struct {
	Region     string
	BucketName string
	Config     *S3Config
	Args       map[string]string
	Sess       *session.Session
	buffer     *buffer
}

type S3Config struct {
	Folder         string
	CommitFileSize int
	CommitDuration int
	UploadEvery    int
}

type buffer struct {
	path     string
	messages chan string
}

func (s *S3) Connect() (err error) {
	s.buffer = &buffer{}
	// overwrite buffer.path with Args, if specified
	if val, ok := s.Args["bufferPath"]; ok {
		s.buffer.path = val
	} else {
		// default
		s.buffer.path = "/tmp/manifold/aws_s3/"
	}

	// create messages channel
	s.buffer.messages = make(chan string, 1000)
	// create a collector
	go s.collector()
	// create an uploader
	go s.uploader()

	return
}

func (s *S3) Disconnect() (err error) {
	close(s.buffer.messages)
	return
}

func (s *S3) Write(message string) (err error) {
	s.buffer.messages <- message
	return
}

func (s *S3) Info() {
	log.Info("S3.BucketName: ", s.BucketName)
	log.Infof("S3Config.CommitFileSize: every %d KB\n", s.Config.CommitFileSize)
	log.Infof("S3Config.CommitDuration: every %d minutes\n", s.Config.CommitDuration)
	log.Infof("S3Config.UploadEvery: %d seconds\n", s.Config.UploadEvery)
}

// Receive data on messages channel and write them
// to buf.path.
//
// Files are aggregated on a 5 minutes interval.
func (s *S3) collector() {
	// create buf.path if it doesn't exist
	err := os.MkdirAll(s.buffer.path, os.ModePerm)
	if err != nil {
		log.Fatal(err)
	}

	bufferPath := filepath.Join(s.buffer.path, "buffer")

	// read messages from channel and write them to a file
	go func(bufferPath string) {
		for {
			// read buf.messages channel
			msg, ok := <-s.buffer.messages
			if ok == false {
				return // channel closed
			}

			// append (or create) to buffer
			err = swissarmy.AppendFile(bufferPath, msg+"\n")
			if err != nil {
				log.Fatal(err)
			}
		}
	}(bufferPath)

	// roll files
	go func(bufferPath string) {
		timeCommitted := time.Now()
		for {
			// check if file 'buffer' exists
			exists, err := swissarmy.FileExists(bufferPath)
			if err != nil {
				log.Fatal(err)
			}

			if !exists {
				// one second interval loop
				time.Sleep(1 * time.Second)
				continue
			}

			// commit buffer if it's >= Config.CommitFileSize KB
			// or time elapsed >= Config.CommitDuration minutes
			info, err := os.Stat(bufferPath)
			fileSizeReached := info.Size() >= int64(s.Config.CommitFileSize)*1024
			durationElapsed := int(time.Since(timeCommitted).Minutes()) >= s.Config.CommitDuration
			if fileSizeReached || durationElapsed {
				// current point in time
				currentTime := time.Now()
				// organize buffer by creating a folder for each day
				commitDir := filepath.Join(s.buffer.path, currentTime.Format("2006-01-02"))
				// create the day directory if it doesn't exists
				err := os.MkdirAll(commitDir, os.ModePerm)
				if err != nil {
					log.Fatal(err)
				}

				// rename buffer to the current time in nanoseconds
				commitPath := filepath.Join(commitDir, currentTime.Format("150405.000000000"))
				err = os.Rename(bufferPath, commitPath)
				if err != nil {
					log.Fatal(err)
				}

				timeCommitted = time.Now()
				log.Info("Committed file ", commitPath)
			}
		}
	}(bufferPath)
}

// Scan buf.path for files and upload them once found.
func (s *S3) uploader() {
	uploader := s3manager.NewUploader(s.Sess)
	for {
		// check if folder exists
		exists, err := swissarmy.DirExists(s.buffer.path)
		if err != nil {
			log.Fatal(err)
		}

		if !exists {
			// one second interval loop
			time.Sleep(1 * time.Second)
			continue
		}

		var files []string
		err = filepath.Walk(s.buffer.path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.Error("Walkpath error: ", err)
				return err
			}
			if info.IsDir() || info.Name() == "buffer" {
				return nil
			}

			files = append(files, path)

			return nil
		})
		if err != nil {
			panic(err)
		}
		for _, file := range files {
			// truncate buf.path (S3 path)
			key := strings.Replace(file, s.buffer.path, "", 1)
			// prefix it with Config.Folder
			key = filepath.Join(s.Config.Folder, key)
			// read file
			body, err := ioutil.ReadFile(file)
			if err != nil {
				log.Fatalln("Couldn't read file: ", file)
			}
			// upload the file to S3
			_, err = uploader.Upload(&s3manager.UploadInput{
				Bucket: aws.String(s.BucketName),
				Key:    aws.String(key),
				Body:   bytes.NewReader(body),
			})
			if err != nil {
				log.Fatalln("Failed to upload file: ", file)
			}
			// file uploaded successfully
			err = os.Remove(file)
			if err != nil {
				log.Errorln("Couldn't remove file: ", file)
			}

			log.Info("Uploaded ", key)
		}
		time.Sleep(time.Duration(s.Config.UploadEvery) * time.Second)
	}
}