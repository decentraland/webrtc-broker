# BUILD STAGE
FROM golang:1.11.1 as builder

WORKDIR /usr/src/app

# Install app dependencies
COPY . .

RUN go get -d -v ./...
RUN go install -v ./...
RUN make test

# DEPLOY STAGE
FROM golang:1.11.1

WORKDIR /root

# Bundle app source
COPY --from=builder /usr/src/app .

EXPOSE 9090
