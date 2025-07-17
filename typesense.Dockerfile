# typesense.Dockerfile

# Start from the official Typesense image
FROM typesense/typesense:29.0

# The official image is based on Debian, so we use apt-get to install curl.
# We then clean up the apt cache to keep the final image small.
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*