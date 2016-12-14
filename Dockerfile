#export TAG=`date +"%Y%m%d.%H%M"`
#export NAME=mgrast/awe
#docker build --force-rm --no-cache --rm -t ${NAME}:${TAG} .


#FROM golang:1.7.4-alpine
FROM golang:1.8


# needed for GIT_COMMIT_HASH
#RUN apk update && apk add git gcc libc-dev cyrus-sasl-dev
RUN apt-get update && apt-get install -y git libsasl2-dev


ENV AWE=/go/src/github.com/MG-RAST/AWE
WORKDIR /go/bin

COPY . /go/src/github.com/MG-RAST/AWE

# backwards compatible pathing with old dockerfile
RUN ln -s /go /gopath

# compile AWE
RUN mkdir -p ${AWE} && \
  cd ${AWE} && \
  go get -d ./awe-client/ ./awe-server/ && \
  ./compile.sh

# since this produces three binaries, we just specify (b)ash
CMD ["/bin/bash"]
#CMD ["/bin/ash"]
