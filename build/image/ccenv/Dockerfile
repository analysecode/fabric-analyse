# Copyright Greg Haskins All Rights Reserved
#
# SPDX-License-Identifier: Apache-2.0
#
FROM hyperledger/fabric-baseimage:x86_64-0.3.2
COPY payload/chaintool payload/protoc-gen-go /usr/local/bin/
ADD payload/goshim.tar.bz2 $GOPATH/src/
RUN mkdir -p /chaincode/input /chaincode/output
LABEL org.hyperledger.fabric.version=1.0.6-snapshot-70a7ce5b \
      org.hyperledger.fabric.base.version=0.3.2
