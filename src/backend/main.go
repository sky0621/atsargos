package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/slack-go/slack"
	"golang.org/x/image/draw"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

func main() {
	/*
	 * 必須情報を環境変数から取得
	 */
	projectID := os.Getenv("PROJECT_ID")
	if projectID == "" {
		log.Fatal("no PROJECT_ID")
	}
	bucketName := os.Getenv("BUCKET_NAME")
	if bucketName == "" {
		log.Fatal("no BUCKET_NAME")
	}
	cred := os.Getenv("SA_CREDENTIALS")
	if cred == "" {
		log.Fatal("no SA_CREDENTIALS")
	}
	slackToken := os.Getenv("SLACK_API_TOKEN")
	if slackToken == "" {
		log.Fatal("no SLACK_API_TOKEN")
	}

	/*
	 * GCSの署名付きURL生成関数実行用の設定
	 */
	conf, err := google.JWTConfigFromJSON([]byte(cred), storage.ScopeReadOnly)
	if err != nil {
		log.Fatal(err)
	}
	opts := &storage.SignedURLOptions{
		GoogleAccessID: conf.Email,
		PrivateKey:     conf.PrivateKey,
		Method:         http.MethodGet,
	}
	signedURLFunc := func(fileName string, expires time.Time) (string, error) {
		opts.Expires = expires
		url, err := storage.SignedURL(bucketName, fileName, opts)
		if err != nil {
			fmt.Println(err)
			return "", err
		}

		return url, nil
	}

	ctx := context.Background()

	/*
	 * GCSアクセス用クライアント生成
	 */
	storageCli, err := storage.NewClient(ctx, option.WithCredentialsJSON([]byte(cred)))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if storageCli != nil {
			if err := storageCli.Close(); err != nil {
				fmt.Println(err)
			}
		}
	}()

	// GCSへの画像ファイルアップロード関数
	uploadGCSObjectFunc := func(ctx context.Context, objectName string, reader io.Reader) error {
		rImg, err := resizedImage(reader)
		if err != nil {
			return fmt.Errorf("resizedImage: %v", err)
		}
		writer := storageCli.Bucket(bucketName).Object(objectName).NewWriter(ctx)
		defer func() {
			if writer != nil {
				if err := writer.Close(); err != nil {
					fmt.Println(err)
				}
			}
		}()
		writer.ContentType = "image/png"
		if _, err = io.Copy(writer, rImg); err != nil {
			return fmt.Errorf("io.Copy: %v", err)
		}
		return nil
	}

	// GCSからの画像ファイル削除関数
	deleteGCSObjectFunc := func(ctx context.Context, objectName string) error {
		if err := storageCli.Bucket(bucketName).Object(objectName).Delete(ctx); err != nil {
			fmt.Println(err)
			return err
		}
		return nil
	}

	/*
	 * Firestoreアクセス用クライアント生成
	 */
	firestoreCli, err := firestore.NewClient(ctx, projectID, option.WithCredentialsJSON([]byte(cred)))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if firestoreCli != nil {
			if err := firestoreCli.Close(); err != nil {
				fmt.Println(err)
			}
		}
	}()

	// Slack API クライアント
	slackCli := slack.New(slackToken, slack.OptionDebug(true))

	/*
	 * Web APIサーバーとしての設定
	 */
	var e *echo.Echo
	{
		e = echo.New()
		e.Use(middleware.Logger())
		e.Use(middleware.Recover())
		e.Use(middleware.CORS())

		e.GET("/*", static())
		e.GET("/api/list", list(firestoreCli, signedURLFunc))
		e.POST("/api/addImage", addImage(firestoreCli, uploadGCSObjectFunc))
		e.PUT("/api/updateImage", updateImage(firestoreCli, uploadGCSObjectFunc, deleteGCSObjectFunc))
		e.PUT("/api/deleteImage", deleteImage(firestoreCli, deleteGCSObjectFunc))
		e.GET("/api/notify", notify(firestoreCli, slackCli))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if err := e.Start(":" + port); err != nil {
		log.Fatal(err)
	}
}

// 静的ルート用
func static() echo.HandlerFunc {
	return func(c echo.Context) error {
		wd, err := os.Getwd()
		if err != nil {
			log.Println(err)
			return err
		}
		fs := http.FileServer(http.Dir(filepath.Join(wd, "view")))
		fs.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

func addImage(firestoreCli *firestore.Client, uploadGCSObjectFunc uploadGCSObjectFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		date := c.FormValue("date")
		name := c.FormValue("name")
		notifyStr := c.FormValue("notify")
		notify, err := strconv.Atoi(notifyStr)
		if err != nil {
			fmt.Println(err)
			if !strings.Contains(err.Error(), "no such file") {
				return c.String(http.StatusInternalServerError, err.Error())
			}
		}

		imageFile, err := c.FormFile("imageFile")
		if err != nil {
			fmt.Println(err)
			if !strings.Contains(err.Error(), "no such file") {
				return c.String(http.StatusInternalServerError, err.Error())
			}
		}

		id := uuid.New().String()

		if imageFile != nil {
			f, err := imageFile.Open()
			if err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}

			if err := uploadGCSObjectFunc(c.Request().Context(), id, f); err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}
		}

		_, err = firestoreCli.Collection("image").Doc(id).Set(c.Request().Context(),
			map[string]interface{}{
				"id":     id,
				"date":   date,
				"name":   name,
				"notify": notify,
			},
		)
		if err != nil {
			fmt.Println(err)
			return c.String(http.StatusInternalServerError, err.Error())
		}

		return nil
	}
}

func updateImage(firestoreCli *firestore.Client, uploadGCSObjectFunc uploadGCSObjectFunc, deleteGCSObjectFunc deleteGCSObjectFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.FormValue("id")
		date := c.FormValue("date")
		name := c.FormValue("name")
		notifyStr := c.FormValue("notify")
		notify, err := strconv.Atoi(notifyStr)
		if err != nil {
			fmt.Println(err)
			if !strings.Contains(err.Error(), "no such file") {
				return c.String(http.StatusInternalServerError, err.Error())
			}
		}

		imageFile, err := c.FormFile("imageFile")
		if err != nil {
			fmt.Println(err)
			if !strings.Contains(err.Error(), "no such file") {
				return c.String(http.StatusInternalServerError, err.Error())
			}
		}

		if imageFile != nil {
			f, err := imageFile.Open()
			if err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}

			if err := uploadGCSObjectFunc(c.Request().Context(), id, f); err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}
		}

		_, err = firestoreCli.Collection("image").Doc(id).Update(c.Request().Context(),
			[]firestore.Update{
				{Path: "date", Value: date},
				{Path: "name", Value: name},
				{Path: "notify", Value: notify},
			},
		)
		if err != nil {
			fmt.Println(err)
			return c.String(http.StatusInternalServerError, err.Error())
		}

		return nil
	}
}

func deleteImage(firestoreCli *firestore.Client, deleteGCSObjectFunc deleteGCSObjectFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.FormValue("id")

		_, err := firestoreCli.Collection("image").Doc(id).Delete(c.Request().Context())
		if err != nil {
			fmt.Println(err)
			return c.String(http.StatusInternalServerError, err.Error())
		}

		if err := deleteGCSObjectFunc(c.Request().Context(), id); err != nil {
			fmt.Println(err)
			return c.String(http.StatusInternalServerError, err.Error())
		}

		return nil
	}
}

func notify(firestoreCli *firestore.Client, slackCli *slack.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		iter := firestoreCli.Collection("image").Documents(c.Request().Context())
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return err
			}
			var image *Image
			if err := doc.DataTo(&image); err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}

			iDate, err := time.Parse("2006-01-02", image.Date)
			if err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}
			if image.Notify > 0 {
				if iDate.AddDate(0, 0, image.Notify-1).Before(time.Now()) {
					_, _, _, err := slackCli.SendMessageContext(c.Request().Context(), "general", slack.MsgOptionText(fmt.Sprintf("[%s][%s]", image.Name, image.Date), false))
					if err != nil {
						fmt.Printf("%#v\n", err)
						return err
					}
				}
			}
		}
		return nil
	}
}

func list(firestoreCli *firestore.Client, signedURLFunc signedURLFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		iter := firestoreCli.Collection("image").Documents(c.Request().Context())
		var images []*Image
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return err
			}
			var image *Image
			if err := doc.DataTo(&image); err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}
			url, err := signedURLFunc(image.ID, time.Now().Add(30*time.Minute))
			if err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}
			image.URL = url
			images = append(images, image)
		}
		return c.JSON(http.StatusOK, images)
	}
}

// GCSオブジェクトアップロード用関数
type uploadGCSObjectFunc func(ctx context.Context, objectName string, reader io.Reader) error

// GCSオブジェクト削除用関数
type deleteGCSObjectFunc func(ctx context.Context, objectName string) error

// 署名付きURL生成用関数
type signedURLFunc func(fileName string, expires time.Time) (string, error)

type Image struct {
	ID     string `json:"id"`
	Date   string `json:"date"`
	Name   string `json:"name"`
	Notify int    `json:"notify"`

	URL string `json:"url"`
}

func resizedImage(r io.Reader) (io.Reader, error) {
	imgSrc, imgType, err := image.Decode(r)
	if err != nil {
		return nil, err
	}

	rctSrc := imgSrc.Bounds()

	var imgDst *image.RGBA
	{
		dx := rctSrc.Dx()
		dy := rctSrc.Dy()
		for dx > 640 {
			dx = dx / 2
			dy = dy / 2
		}
		imgDst = image.NewRGBA(image.Rect(0, 0, dx, dy))
	}
	draw.CatmullRom.Scale(imgDst, imgDst.Bounds(), imgSrc, rctSrc, draw.Over, nil)

	bf := &bytes.Buffer{}
	switch imgType {
	case "png":
		if err := png.Encode(bf, imgDst); err != nil {
			return nil, err
		}
	case "jpeg":
		if err := jpeg.Encode(bf, imgDst, &jpeg.Options{Quality: 100}); err != nil {
			return nil, err
		}
	case "gif":
		if err := gif.Encode(bf, imgDst, nil); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}

	return bf, nil
}
