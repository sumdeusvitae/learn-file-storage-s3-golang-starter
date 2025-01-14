package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxSize = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	videoDB, err := cfg.db.GetVideo(videoID)
	if err != nil {

		respondWithError(w, http.StatusInternalServerError, "Couldn't get video", err)
		return
	}

	if videoDB.UserID != userID {

		respondWithError(w, http.StatusUnauthorized, "User not authorized", nil)
		return
	}

	err = r.ParseMultipartForm(maxSize)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form", err)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get file", err)
		return
	}
	defer file.Close()

	// Get the content type of the uploaded file
	contentType := header.Header.Get("Content-Type")

	// Validate if the content type is "video/mp4"
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type. Only MP4 videos are allowed", err)
		return
	}

	// save local temp
	f, err := os.CreateTemp("/tmp", "temp-*.mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't create temp local file", err)
		return
	}
	defer os.Remove(f.Name()) // clean up
	defer f.Close()
	if _, err := io.Copy(f, file); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't copy temp local file", err)
		return
	}
	f.Seek(0, io.SeekStart)

	// Prefix
	ratio, err := getVideoAspectRatio(f.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get ratio", err)
		return
	}

	// Create a 32-byte slice
	randomBytes := make([]byte, 32)
	// Fill the slice with random bytes
	_, err = rand.Read(randomBytes)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to generate random bytes", err)
		return
	}

	var fileName = fmt.Sprintf("%s/%s.%s", ratio, hex.EncodeToString(randomBytes), strings.Split(mediaType, "/")[1])

	// Fast start
	processedVideo, err := processVideoForFastStart(f.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Video can't be processed", err)
		return
	}
	f, err = os.Open(processedVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't open processed video", err)
	}
	defer os.Remove(processedVideo)
	defer f.Close()

	var putObjectInput = s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileName,
		Body:        f,
		ContentType: &contentType,
	}
	_, err = cfg.s3Client.PutObject(context.Background(), &putObjectInput)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload video", err)
		return
	}

	// signed URL
	// video_url := fmt.Sprintf("%s,%s", cfg.s3Bucket, fileName)

	// db update
	// var s3BucketURL = fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileName)

	// using Cloudfront
	var s3BucketURL = fmt.Sprintf("https://%s/%s", cfg.s3CfDistribution, fileName)

	videoDB.VideoURL = &s3BucketURL // video_url
	err = cfg.db.UpdateVideo(videoDB)
	if err != nil {

		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}
	/*
		videoDB, err = cfg.dbVideoToSignedVideo(videoDB)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "Couldn't sign video URL", err)
			return
		}
	*/

	respondWithJSON(w, http.StatusOK, videoDB)

}

/*
func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	// Create a presign client
	presignClient := s3.NewPresignClient(s3Client)
	// Specify the parameters for the presigned URL (e.g., getting an object)
	req := &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}

	presignResp, err := presignClient.PresignGetObject(context.TODO(), req, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", fmt.Errorf("error generating presigned url: %v", err)
	}
	return presignResp.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	parts := strings.Split(*video.VideoURL, ",")
	if len(parts) != 2 {
		return database.Video{}, fmt.Errorf("invalid video URL format: expected 'bucket,key' but got %d parts", len(parts))
	}
	bucket, key := parts[0], parts[1]
	presignedURL, err := generatePresignedURL(cfg.s3Client, bucket, key, time.Hour)
	if err != nil {
		return database.Video{}, fmt.Errorf("couldn't map db video to video with presigned url: %v", err)
	}
	video.VideoURL = &presignedURL
	return video, nil
}
*/
