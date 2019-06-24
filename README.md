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
You can use `export-dynamodb` program to export DynamoDB entries as CSV. Run
`make dynamoexport` in project directory and it will spit out a `output.csv`.

## Flaws
Following are some known flaws in `EventsProcessor` function:
* Library for parsing EXIF data is not as widely used compared to other libs in JS
landscape.
* Library for parsing XMP data is not as widely used compared to other libs in JS
landscape.
* We're throwing an error if the image has more than one instance XMP document
(opening and closing tags). But it is technically possible to have more than documents
in the latest specs.
* We're trying to get `string` value for most of the properties. Which results in
sub-optimal representations some times.
* There is some problem with XP tags representation in EXIF data.
