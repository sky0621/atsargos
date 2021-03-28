package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
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
		fmt.Printf("signedURL:%s\n", url)

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
	uploadGCSObjectFunc := func(ctx context.Context, objectName string, reader io.Reader) error {
		writer := storageCli.Bucket(bucketName).Object(objectName).NewWriter(ctx)
		defer func() {
			if writer != nil {
				if err := writer.Close(); err != nil {
					fmt.Println(err)
				}
			}
		}()
		writer.ContentType = "image/png"
		if _, err = io.Copy(writer, reader); err != nil {
			return fmt.Errorf("io.Copy: %v", err)
		}
		return nil
	}
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

	/*
	 * Web APIサーバーとしての設定
	 */
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	e.GET("/*", static())
	e.GET("/api/list", list(firestoreCli, signedURLFunc))
	e.POST("/api/addImage", addImage(firestoreCli, uploadGCSObjectFunc))
	e.PUT("/api/updateImage", updateImage(firestoreCli, uploadGCSObjectFunc, deleteGCSObjectFunc))
	e.PUT("/api/deleteImage", deleteImage(firestoreCli, deleteGCSObjectFunc))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		fmt.Printf("Defaulting to port %s\n", port)
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
		name := c.FormValue("name")
		fmt.Printf("name:%s\n", name)

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
			fmt.Printf("imageFile.Filename:%s\n", imageFile.Filename)

			if err := uploadGCSObjectFunc(c.Request().Context(), id, f); err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}
		}

		_, err = firestoreCli.Collection("image").Doc(id).Set(c.Request().Context(),
			map[string]interface{}{
				"id":   id,
				"name": name,
				"date": time.Now().Format("2006-01-02"),
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
		fmt.Printf("id:%s\n", id)

		imageFile, err := c.FormFile("imageFile")
		if err != nil {
			fmt.Println(err)
			if !strings.Contains(err.Error(), "no such file") {
				return c.String(http.StatusInternalServerError, err.Error())
			}
		}

		if imageFile == nil {
			if err := deleteGCSObjectFunc(c.Request().Context(), id); err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}
		} else {
			f, err := imageFile.Open()
			if err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}
			fmt.Printf("imageFile.Filename:%s\n", imageFile.Filename)

			if err := uploadGCSObjectFunc(c.Request().Context(), id, f); err != nil {
				fmt.Println(err)
				return c.String(http.StatusInternalServerError, err.Error())
			}
		}

		_, err = firestoreCli.Collection("image").Doc(id).Update(c.Request().Context(),
			[]firestore.Update{
				{Path: "date", Value: time.Now().Format("2006-01-02")},
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
		fmt.Printf("id:%s\n", id)

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
	ID   string `json:"id"`
	Date string `json:"date"`
	Name string `json:"name"`

	URL string `json:"url"`
}
