package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pkg/errors"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/mknote"
	"log"
	"os"
)

const (
	bucketName = "skycatch-test-photos"
)

func init() {
	exif.RegisterParsers(mknote.All...)
}

// Handler is our lambda handler invoked by the `lambda.Start` function call
func Handler(ctx context.Context) error {
	cfg, err := external.LoadDefaultAWSConfig()
	if err != nil {
		return err
	}
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") == "test" {
		cfg.EndpointResolver = aws.ResolveWithEndpointURL("http://host.docker.internal:8000")
	}
	var db = dynamodb.New(cfg)

	data, keys, err := scan(ctx, db, nil)

	data = append(data, []string{})
	copy(data[1:], data)
	data[0] = keys

	var body = bytes.NewBuffer([]byte{})
	w := csv.NewWriter(body)
	if err := w.WriteAll(data); err != nil {
		log.Printf("error writing csv: %v", err)
		return err
	}

	var s3Client = s3.New(cfg)

	_, err = s3Client.PutObjectRequest(&s3.PutObjectInput{
		Bucket:        aws.String(bucketName),
		Key:           aws.String("image-data.csv"),
		ContentType:   aws.String("text/csv"),
		Body:          bytes.NewReader(body.Bytes()),
		ContentLength: aws.Int64(int64(body.Len())),
		ContentDisposition: aws.String("attachment"),
	}).Send(ctx)
	if err != nil {
		log.Printf("error while uploading CSV to S3: %v", err)
		return err
	}

	return nil
}

func scan(ctx context.Context, db *dynamodb.Client, lek map[string]dynamodb.AttributeValue) ([][]string, []string, error) {
	var data = [][]string{}
	var keysMap = map[string]bool{}

	var scanParams = &dynamodb.ScanInput{
		TableName: aws.String(os.Getenv("DYNAMO_TABLE_IMAGE_DATA")),
	}
	if lek != nil {
		scanParams.ExclusiveStartKey = lek
	}
	sr, err := db.ScanRequest(scanParams).Send(ctx)
	if err != nil {
		log.Printf("error while scanning: %v", err)
		return nil, nil, err
	}

	if *sr.Count < 1 {
		log.Printf("no records found in scan")
		return nil, nil, errors.New("no records found")
	}

	for _, i := range sr.Items {
		var item = []string{}
		for k, v := range i {
			keysMap[k] = true
			if v.NULL != nil && *v.NULL != false {
				item = append(item, "")
			} else {
				item = append(item, *v.S)
			}
		}
		data = append(data, item)
	}
	var keys = []string{}
	for k, _ := range keysMap {
		keys = append(keys, k)
	}

	if len(sr.LastEvaluatedKey) < 1 {
		newData, newKeys, err := scan(ctx, db, sr.LastEvaluatedKey)
		if err != nil {
			return nil, nil, err
		}

		data = append(data, newData...)
		keys = append(keys, newKeys...)
	}

	return data, keys, err
}

func main() {
	lambda.Start(Handler)
}
