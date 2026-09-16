# JBIG2 [![PkgGoDev](https://pkg.go.dev/badge/github.com/xiaoqidun/jbig2)](https://pkg.go.dev/github.com/xiaoqidun/jbig2)
高性能、纯 Go 语言实现的 JBIG2 图像编解码库

# 安装指南
```shell
go get -u github.com/xiaoqidun/jbig2
```

# 解码全部
```go
package main

import (
	"fmt"
	"image/png"
	"log"
	"os"

	"github.com/xiaoqidun/jbig2"
)

func main() {
	// 1. 打开JB2文件
	file, err := os.Open("test.jb2")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	// 2. 创建JB2解码
	dec, err := jbig2.NewDecoder(file)
	if err != nil {
		log.Fatal(err)
	}
	// 3. 解码全部图像
	images, err := dec.DecodeAll()
	if err != nil {
		log.Fatal(err)
	}
	// 4. 输出全部图像
	for i, img := range images {
		outName := fmt.Sprintf("test_%d.png", i)
		outFile, err := os.Create(outName)
		if err != nil {
			log.Fatal(err)
		}
		if err := png.Encode(outFile, img); err != nil {
			outFile.Close()
			log.Fatal(err)
		}
		outFile.Close()
		log.Printf("已输出第 %d 页到 %s\n", i, outName)
	}
}
```

# 解码首页
```go
package main

import (
	"image/png"
	"log"
	"os"

	"github.com/xiaoqidun/jbig2"
)

func main() {
	// 1. 打开JB2文件
	file, err := os.Open("test.jb2")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	// 2. 解码首页图像
	img, err := jbig2.Decode(file)
	if err != nil {
		log.Fatal(err)
	}
	// 3. 输出PNG文件
	outFile, err := os.Create("test.png")
	if err != nil {
		log.Fatal(err)
	}
	defer outFile.Close()
	if err := png.Encode(outFile, img); err != nil {
		log.Fatal(err)
	}
	log.Printf("宽度: %d, 高度: %d, 已输出到 test.png\n", img.Bounds().Dx(), img.Bounds().Dy())
}
```

# 标准用法
```go
package main

import (
	"image"
	"image/png"
	"log"
	"os"

	_ "github.com/xiaoqidun/jbig2"
)

func main() {
	// 1. 打开JB2文件
	file, err := os.Open("test.jb2")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	// 2. 标准方式解码
	img, format, err := image.Decode(file)
	if err != nil {
		log.Fatal(err)
	}
	// 3. 输出PNG文件
	outFile, err := os.Create("test.png")
	if err != nil {
		log.Fatal(err)
	}
	defer outFile.Close()
	if err := png.Encode(outFile, img); err != nil {
		log.Fatal(err)
	}
	log.Printf("格式: %s, 宽度: %d, 高度: %d, 已输出到 test.png\n", format, img.Bounds().Dx(), img.Bounds().Dy())
}
```

# 编码全部
```go
package main

import (
	"image"
	"image/png"
	"log"
	"os"

	"github.com/xiaoqidun/jbig2"
)

func main() {
	fileNames := []string{"test_0.png", "test_1.png"}
	var images []image.Image
	for _, name := range fileNames {
		file, err := os.Open(name)
		if err != nil {
			log.Fatal(err)
		}
		img, err := png.Decode(file)
		file.Close()
		if err != nil {
			log.Fatal(err)
		}
		bitmap, err := jbig2.Binarize(img, 128)
		if err != nil {
			log.Fatal(err)
		}
		images = append(images, bitmap)
	}
	outFile, err := os.Create("test.jb2")
	if err != nil {
		log.Fatal(err)
	}
	if err := jbig2.EncodeAll(outFile, images, nil); err != nil {
		outFile.Close()
		log.Fatal(err)
	}
	if err := outFile.Close(); err != nil {
		log.Fatal(err)
	}
	log.Printf("已编码 %d 页到 test.jb2\n", len(images))
}
```

# 编码单页
```go
package main

import (
	"image/png"
	"log"
	"os"

	"github.com/xiaoqidun/jbig2"
)

func main() {
	file, err := os.Open("test.png")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		log.Fatal(err)
	}
	bitmap, err := jbig2.Binarize(img, 128)
	if err != nil {
		log.Fatal(err)
	}
	outFile, err := os.Create("test.jb2")
	if err != nil {
		log.Fatal(err)
	}
	if err := jbig2.Encode(outFile, bitmap, nil); err != nil {
		outFile.Close()
		log.Fatal(err)
	}
	if err := outFile.Close(); err != nil {
		log.Fatal(err)
	}
	log.Printf("宽度: %d, 高度: %d, 已输出到 test.jb2\n", bitmap.Bounds().Dx(), bitmap.Bounds().Dy())
}
```

# 授权协议
本项目使用 [Apache License 2.0](https://github.com/xiaoqidun/jbig2/blob/main/LICENSE) 授权协议