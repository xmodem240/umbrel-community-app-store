FROM python:3.12-slim
ENV PYTHONUNBUFFERED=1 DATA_DIR=/data PORT=8080
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY server.py boards.json models_extra.json ./
COPY static ./static
EXPOSE 8080
CMD ["python", "server.py"]
