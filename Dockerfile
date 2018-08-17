# Copyright 2017 clair authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM golang:1.10-alpine AS build-clair
ADD .   /go/src/github.com/coreos/clair/
WORKDIR /go/src/github.com/coreos/clair/
RUN go build github.com/coreos/clair/cmd/clair

FROM alpine:3.8 AS build-qcowmount
RUN apk add --no-cache wget build-base fuse-dev
RUN wget https://github.com/libyal/libqcow/releases/download/20170222/libqcow-alpha-20170222.tar.gz && tar xzvf libqcow-alpha-20170222.tar.gz
RUN cd libqcow-20170222 && ./configure && make && make install

FROM ubuntu:18.04 as build-lklfuse
RUN apt-get update && apt-get -y install build-essential  libfuse-dev libarchive-dev python xfsprogs git bc bison flex
RUN git clone --depth=1 https://github.com/libos-nuse/lkl-linux.git
RUN cd lkl-linux && make -C tools/lkl && make -C tools/lkl  install

FROM frolvlad/alpine-glibc
COPY --from=build-clair /go/src/github.com/coreos/clair/clair /clair
COPY --from=build-qcowmount /usr/local/bin/qcowmount /usr/bin/qcowmount
COPY --from=build-qcowmount /usr/local/lib /usr/local/lib
COPY --from=build-lklfuse /lkl-linux/tools/lkl/lklfuse /usr/bin/lklfuse
COPY --from=build-lklfuse /lkl-linux/tools/lkl/liblkl.so /usr/lib/liblkl.so
RUN apk add --no-cache git rpm xz ca-certificates dumb-init fuse parted
ENTRYPOINT ["/usr/bin/dumb-init", "--", "/clair"]
VOLUME /config
EXPOSE 6060 6061
