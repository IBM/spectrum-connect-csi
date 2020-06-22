#!/bin/bash -x

echo "JUNIT OUTPUT"
echo ${JUNIT_OUTPUT} ${SECRET_FILE} ${PARAM_FILE} ${ENDPOINT} ${TESTS_TO_RUN_FILE}


echo "TESTT ${STORAGE_ARRAYS}  ${USERNAME}  ${PASSWORD}"

# update CSI secret
sed -i -e "s/STORAGE_ARRAYS/${STORAGE_ARRAYS}/g" ${SECRET_FILE}
sed -i -e "s/USERNAME/${USERNAME}/g" ${SECRET_FILE}
sed -i -e "s/PASSWORD/${PASSWORD}/g" ${SECRET_FILE}

echo "TESTT ${STORAGE_ARRAYS}  ${USERNAME}  ${PASSWORD}"

# update params file
sed -i -e "s/POOL_NAME/${POOL_NAME}/g" ${PARAM_FILE}

# get tests to run
TESTS=`cat ${TESTS_TO_RUN_FILE}| awk '{$1=$1};1' |tr [:space:] "|"`

/usr/local/go/src/github.com/kubernetes-csi/csi-test/cmd/csi-sanity/csi-sanity  --csi.endpoint ${ENDPOINT} --csi.controllerendpoint ${ENDPOINT_CONTROLLER} --csi.secrets ${SECRET_FILE} --csi.testvolumeparameters ${PARAM_FILE}  --csi.junitfile ${JUNIT_OUTPUT} --ginkgo.focus ${TESTS}
