# Skycatch project
This is evautatory project for a job interview at Skycatch. 

## Architecture
The system is based on AWS S3, AWS DynamoDB and AWS Lambda. 

### Data extraction
The S3 bucket is configured as a trigger for `EventsProcessor` Lambda function. When
you upload a file to the bucket it triggers an S3 event. The Lambda function uses the
data in event to fetch the related files from S3. Then it does some basic validations
to make sure that it is a valid file. And then it extracts EXIF and XMP data and puts
the data into DynamoDB using `etag` as primary key.

### Exporting as CSV
There are two ways to export data in CSV format.
* I've implemented a Lambda function `CSVExporter` that scans the DynamoDB table,
creates CSV file and puts it into S3 bucket. You can invoke the function directly from
the CLI by running `aws lambda invoke --function-name FUNCTION_ARN outfile $1`.
* Other way is to use `export-dynamodb` program. Run `make dynamoexport` in project
directory.
