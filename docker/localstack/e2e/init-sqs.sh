#!/bin/sh
set -eu

awslocal sqs create-queue --queue-name categorization-e2e >/dev/null
awslocal sqs create-queue --queue-name notification-e2e >/dev/null
