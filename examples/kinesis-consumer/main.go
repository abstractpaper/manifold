package main

import (
	"os"
	"os/signal"

	"github.com/abstractpaper/manifold/stream"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	log "github.com/sirupsen/logrus"
)

func main() {
	// interrupt channel for OS signals
	interrupt := make(chan os.Signal, 1)
	// register interrupt channel to receive SIGINT and SIGKILL
	signal.Notify(interrupt, os.Interrupt, os.Kill)

	// aws config
	awsRegion := "us-east-1"
	awsAccessKey := "XXXXXXXXXXXXXXXXXXXX"
	awsSecretKey := "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"

	// AWS setup
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(awsRegion),
		Credentials: credentials.NewStaticCredentials(awsAccessKey, awsSecretKey, ""),
	})
	if err != nil {
		log.Fatalln("Error creating session: ", err)
	}

	src := stream.Kinesis{
		ConsumerName: "test-consumer",
		StreamARN:    "arn:aws:kinesis:us-east-1:999999999999:stream/test",
		AWSSess:      sess,
	}

	dest := stream.Stdio{}

	stream.Flow(&src, nil, &dest)

	// wait for interrupt signals
	<-interrupt
	log.Info("Interrupt received.")
}
