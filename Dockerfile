FROM alpine:3.23
COPY hello.txt /hello.txt
RUN echo built-inside > /built.txt
