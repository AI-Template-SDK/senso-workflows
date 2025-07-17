# qdrant.Dockerfile

# Start from the official Qdrant image
FROM qdrant/qdrant

# The Qdrant image is also based on Debian, so we use apt-get to install curl
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*