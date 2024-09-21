# karpenter-provider-ibm-cloud

## install operator

## prereqs

ibmcloud api key from gui console

iam api key used by VPC & global catalog APIs

`ibmcloud iam api-key-create MyKey -d "this is my API key" --file key_file`

## to run gen_instance_types.go

```
❯ export VPC_URL=https://us-south.iaas.cloud.ibm.com/v1
❯ export VPC_AUTH_TYPE=iam
❯ export GLOBAL_CATALOG_AUTH_TYPE=iam
❯ export VPC_APIKEY=<redacted>
❯ export GLOBAL_CATALOG_APIKEY=<redacted>
❯ export IBM_CLOUD_API_KEY=<redacted>
```
