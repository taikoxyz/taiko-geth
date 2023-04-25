#!/bin/bash -e

CURDIR="$(dirname $0)"
TAG_DEST=$(git rev-parse --short HEAD)
export CLOUDSDK_CORE_PROJECT=eoscanada-shared-services

pushd ${CURDIR}/.. >/dev/null
  PROJDIR="$(basename $(pwd))-dm"
popd >/dev/null

pushd ${CURDIR} >/dev/null
  gcloud builds submit \
        --config cloudbuild.yaml \
        --timeout 15m \
        --substitutions=TAG_NAME=${TAG_DEST},_APP=${PROJDIR} \
        ..
popd >/dev/null
echo "TAG_DEST: ${TAG_DEST}"

