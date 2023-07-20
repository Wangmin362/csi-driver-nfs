# Copyright 2020 The Kubernetes Authors.
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

FROM m.daocloud.io/registry.k8s.io/build-image/debian-base:bullseye-v1.4.3

RUN apt update && apt upgrade -y && apt-mark unhold libcap2 && clean-install ca-certificates mount nfs-common netbase

ARG ARCH
ARG binary=./bin/${ARCH}/nfsplugin
COPY ${binary} /nfsplugin
COPY ./bin/${ARCH}/dlv /usr/local/bin/dlv
# COPY /root/go/bin/dlv /usr/local/bin/dlv

ENTRYPOINT ["dlv --listen=:12800 --headless=true --api-version=2 --accept-multiclient exec /nfsplugin -- "]
