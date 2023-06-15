# syntax=docker/dockerfile:1
FROM golang:1.20
RUN apt-get update && apt-get install -y build-essential

# Set destination for COPY
RUN mkdir /build
WORKDIR /build

RUN export G0111MODULE=on

RUN cd /build && git clone https://github.com/Sanyasims/whatsmeow.git

RUN cd /build/whatsmeow/webtest && go build

WORKDIR /build/whatsmeow/webtest

ENTRYPOINT "/build/whatsmeow/webtest/webtest"