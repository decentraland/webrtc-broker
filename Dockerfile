# BUILD STAGE
FROM golang:1.12.0 as builder

WORKDIR /usr/src/app

# Install app dependencies
COPY . .

RUN apt-get update && apt-get install -y \
    libssl-dev

RUN go get -d -v ./...
RUN make build
RUN go install -v ./...

# DEPLOY STAGE
FROM golang:1.12.0

RUN apt-get update && apt-get install -y \
    libssl-dev

WORKDIR /root

# Bundle app source
COPY --from=builder /usr/src/app .

EXPOSE 9090
