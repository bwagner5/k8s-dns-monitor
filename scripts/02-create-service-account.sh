#!/usr/bin/env bash

if [ -z "${CLUSTER_NAME}" ]; then
	echo CLUSTER_NAME environment variable is not set
	exit 1
fi
if [ -z "${AWS_ACCOUNT_ID}" ]; then
	echo AWS_ACCOUNT_ID environment variable is not set
	exit 1
fi

eksctl create iamserviceaccount \
  --cluster "${CLUSTER_NAME}" --name k8s-dns-monitor --namespace k8s-dns-monitor \
  --role-name "${CLUSTER_NAME}-k8s-dns-monitor" \
  --attach-policy-arn "arn:aws:iam::${AWS_ACCOUNT_ID}:policy/${CLUSTER_NAME}-k8s-dns-monitor-policy" \
  --role-only \
  --approve
