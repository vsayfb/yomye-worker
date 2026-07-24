#!/bin/bash

set -euo pipefail

echo "----------- Initializing LocalStack -----------"

echo "Creating SQS queue..."

awslocal sqs create-queue \
    --queue-name gig-category-events

echo "Deploying Lambda..."

awslocal lambda create-function \
    --function-name categorization_service \
    --runtime provided.al2023 \
    --handler bootstrap \
    --zip-file fileb:///etc/localstack/init/ready.d/lambda/function.zip \
    --role arn:aws:iam::000000000000:role/lambda-execution-role

echo "Creating SQS → Lambda event source mapping..."

awslocal lambda create-event-source-mapping \
    --function-name categorization_service \
    --event-source-arn arn:aws:sqs:us-east-1:000000000000:gig-category-events \
    --batch-size 10

echo "----------- LocalStack initialization complete -----------"