package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Xiaoxusheng/web_file/utils"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 定义 ipAccessMap 用于记录 IP 访问情况
var ipAccessMap = make(map[string]struct {
	count          int
	lastAccessTime time.Time
})

// UploadHistory 文件上传历史
type UploadHistory struct {
	Name string    `json:"name"`
	Size int64     `json:"size"`
	Time time.Time `json:"time"`
}

var (
	mutex       sync.Mutex
	channels    = make(chan struct{})
	successChan = make(chan struct{})
	paths       = ""
	// 限制文件大小为20MB
	filesize int64 = 20 << 20

	fileLocks = make(map[string]*sync.Mutex)
	//文件锁
	fileLockMux sync.Mutex
	//存放上传历史记录
	history      []UploadHistory
	historyMutex sync.Mutex
	addr         = ":80"
)

func save(m *multipart.FileHeader) {
	//获取后缀
	suffix := strings.Replace(path.Ext(m.Filename), ".", "", 1)
	fileMap := map[string]bool{
		"mp4": true, "png": true, "jpg": true, "jpeg": true, "gif": true, "docx": true,
	}
	fmt.Println(strings.ToLower(suffix))
	//判断是否在fileMap
	if _, ok := fileMap[strings.ToLower(suffix)]; !ok {
		//不存在
		channels <- struct{}{}
		log.Println("文件格式错误")
		return
	}
	if m.Size > filesize {
		fmt.Println(m.Size, 20<<20)
		channels <- struct{}{}
		log.Println("文件太大")
		return
	}
	var file *os.File
	var err error
	//转化为小写
	if strings.ToLower(suffix) == "docx" {
		file, err = os.Create("./" + m.Filename)
		if err != nil {
			channels <- struct{}{}
			log.Println(err)
			return
		}
	} else {
		//单独保存到文件夹
		file, err = os.Create("/root/file/static/" + m.Filename)
		if err != nil {
			channels <- struct{}{}
			log.Println(err)
			return
		}
	}
	log.Println(m.Filename)

	open, err := m.Open()
	if err != nil {
		channels <- struct{}{}
		log.Println(err)
		return
	}
	defer func(open multipart.File) {
		err := open.Close()
		if err != nil {

		}
	}(open)
	_, err = io.Copy(file, open)
	if err != nil {
		channels <- struct{}{}
		log.Println(err)
		return
	}
	successChan <- struct{}{}
}

func BasicAuth(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, err := os.Open(path.Join(paths, "ip.txt"))
		if err != nil && err != io.EOF {
			log.Println("打开文件失败！")
			return
		}
		defer func(file *os.File) {
			err := file.Close()
			if err != nil {
				log.Println("err:", err)
			}
		}(file)
		//这里获取的是ip地址将端口去掉
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("ipv6", ip)

		IP := make([]byte, 64)
		n, err := file.Read(IP)
		if err != nil {
			log.Println("读取失败！", err)
			return
		}
		fmt.Println("n,ip", n, string(IP[:n]))
		//将字符串转化为ip
		ipAddr := net.ParseIP(ip)
		if ipAddr == nil {
			fmt.Println("Invalid IP address")
			return
		}
		fmt.Println(string(IP), string(IP[:n]) == ipAddr.String())

		user, pass, ok := r.BasicAuth()
		fmt.Println(ok, user, pass, string(IP[:n]) == ipAddr.String())
		if !(ok && string(IP[:n]) == ipAddr.String() && user == "夏雨欣" && pass == "20010405") {
			w.Header().Set("WWW-Authenticate", `Basic realm="restricted"`)
			http.Error(w, "验证失败！非法用户.", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}
}

func set(statusCode int, responseType string, w http.ResponseWriter) {
	if responseType != "" {
		w.Header().Set("Content-Type", responseType)
	}
	w.WriteHeader(statusCode)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	//验证
	user, pass, ok := r.BasicAuth()
	if !(ok && user == "小学生" && pass == "20001205") {
		w.Header().Set("WWW-Authenticate", `Basic realm="restricted"`)
		http.Error(w, "验证失败！非法用户.", http.StatusUnauthorized)
		return
	}
	http.ServeFile(w, r, path.Join(paths, "upload_load.html"))
}

// 获取文件锁
func getFileLock(filename string) *sync.Mutex {
	fileLockMux.Lock()
	defer fileLockMux.Unlock()

	if lock, exists := fileLocks[filename]; exists {
		return lock
	}
	newLock := &sync.Mutex{}
	fileLocks[filename] = newLock
	return newLock
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	filename := r.FormValue("filename")
	//这里加锁是为了防止并发同时创建文件夹
	lock := getFileLock(filename)
	lock.Lock()
	defer lock.Unlock()
	log.Println("进入")

	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Println("err:", err)
		return
	} // 32MB

	file, _, err := r.FormFile("chunk")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer func(file multipart.File) {
		err := file.Close()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	}(file)

	chunkNumber := r.FormValue("chunkNumber")
	totalChunks := r.FormValue("totalChunks")
	log.Println(filename, chunkNumber, totalChunks)

	tempDir := filepath.Join(path.Join(paths, "static"), "temp", filename)
	//创建文件
	err = os.MkdirAll(tempDir, 0755)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk-%s-%s", chunkNumber, totalChunks))
	dst, err := os.Create(chunkPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func(dst *os.File) {
		err := dst.Close()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}(dst)
	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func mergeHandler(w http.ResponseWriter, r *http.Request) {
	type MergeRequest struct {
		Filename string `json:"filename"`
	}
	var req MergeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	//读取临时文件
	tempDir := filepath.Join(path.Join(paths, "static"), "temp", req.Filename)
	chunks, err := os.ReadDir(tempDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	//
	finalPath := filepath.Join(path.Join(paths, "static"), req.Filename)
	finalFile, err := os.Create(finalPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func(finalFile *os.File) {
		err := finalFile.Close()
		if err != nil {

		}
	}(finalFile)
	buf := make([]byte, 32*1024*1024)

	for i := 0; i < len(chunks); i++ {
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk-%d-%d", i, len(chunks)))
		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, err = io.CopyBuffer(finalFile, chunkFile, buf)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = chunkFile.Close()
		if err != nil {
			return
		}
		err = os.Remove(chunkPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	//移除所有临时文件
	err = os.Remove(tempDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fileInfo, _ := os.Stat(finalPath)
	historyMutex.Lock()
	//添加进json
	f, err := os.OpenFile(path.Join(paths, "file.json"), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Println(err)
		return
	}
	file := make([]UploadHistory, 0, 10)
	err = json.NewDecoder(f).Decode(&file)
	if err != nil && !errors.Is(err, io.EOF) {
		log.Println("读取失败", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	file = append(file, UploadHistory{
		Name: req.Filename,
		Size: fileInfo.Size(),
		Time: time.Now(),
	})
	f.Close()
	f, err = os.OpenFile(path.Join(paths, "file.json"), os.O_WRONLY, 0644)
	if err != nil {
		log.Println(err)
		return
	}
	defer f.Close()
	//再次写入
	err = json.NewEncoder(f).Encode(file)
	if err != nil {
		log.Println(err)
		return
	}
	history = append(history, UploadHistory{
		Name: req.Filename,
		Size: fileInfo.Size(),
		Time: time.Now(),
	})
	historyMutex.Unlock()

	w.WriteHeader(http.StatusOK)
}

func historyHandler(w http.ResponseWriter, r *http.Request) {
	historyMutex.Lock()
	defer historyMutex.Unlock()
	//读取file.json
	f, err := os.Open(path.Join(paths, "file.json"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	file := make([]UploadHistory, 0, 10)
	err = json.NewDecoder(f).Decode(&file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Println("file:", file)
	err = json.NewEncoder(w).Encode(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func main() {
	fmt.Println("id", os.Getpid())
	utils.InitMysql()
	defer func() {
		err := recover()
		if err != nil {
			log.Println("出现错误", err)
		}
	}()

	// 设置静态文件目录
	fs := http.FileServer(http.Dir(path.Join(paths, "static")))
	fmt.Println(os.Getwd())

	// 将静态文件目录与指定的路由路径绑定
	http.Handle("/static/", BasicAuth(http.StripPrefix("/static/", fs)))

	//http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "Hello from a HandleFunc #1!\n") })

	http.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		//文件大小不能超过20MB
		err := r.ParseMultipartForm(filesize)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println(err)
			return
		}
		file := r.MultipartForm.File["file"]
		for _, val := range file {
			go save(val)
		}
		i := 0
		fmt.Println(i)
		for i < len(file) {
			select {
			case <-channels:
				i++
			case <-successChan:
				i++
			}
			fmt.Println(i)
		}
		fmt.Println("结束")
		if len(file) == 1 {
			//此处如果是单文件上传，就会跳转
			//多文件没有此功能
			http.Redirect(w, r, filepath.Join("/static", file[0].Filename), http.StatusFound)
			return
		}
		//	重定向到 /
		http.Redirect(w, r, "/static", http.StatusFound)
	})

	http.HandleFunc("/index", func(w http.ResponseWriter, r *http.Request) {
		//	设置响应结构html
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// 设置响应状态码
		w.WriteHeader(http.StatusOK)
		// 设置响应内容
		_, err := io.WriteString(w, "<!DOCTYPE html>\n<html>\n<head>\n    <meta charset=\"UTF-8\">\n    <title>文件上传</title>\n    <style>\n        body {\n            font-family: Arial, sans-serif;\n            background-color: #f5f5f5;\n            margin: 0;\n            padding: 0;\n            display: flex;\n            justify-content: center;\n            align-items: center;\n            height: 100vh;\n        }\n\n        .container {\n            background-color: #fff;\n            border-radius: 5px;\n            padding: 20px;\n            box-shadow: 0 2px 5px rgba(0, 0, 0, 0.3);\n        }\n\n        h1 {\n            text-align: center;\n            color: #333;\n        }\n\n        form {\n            text-align: center;\n            margin-top: 20px;\n        }\n\n        input[type=\"file\"] {\n            display: none;\n        }\n\n        .custom-file-upload {\n            display: inline-block;\n            padding: 10px 20px;\n            cursor: pointer;\n            background-color: #4CAF50;\n            color: #fff;\n            border-radius: 4px;\n            border: none;\n            transition: background-color 0.3s ease;\n        }\n\n        .custom-file-upload:hover {\n            background-color: #45a049;\n        }\n    </style>\n</head>\n<body>\n<div class=\"container\">\n    <h1>文件上传</h1>\n    <form action=\"/upload\" name=\"fail\" method=\"post\" enctype=\"multipart/form-data\">\n        <label for=\"file-upload\" class=\"custom-file-upload\">\n            <i class=\"fa fa-cloud-upload\"></i> 选择文件\n        </label>\n        <input id=\"file-upload\" type=\"file\" name=\"file\" multiple required>\n        <br><br>\n        <input type=\"submit\" value=\"上传\">\n    </form>\n</div>\n</body>\n</html>\n")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	})

	http.HandleFunc("/html", func(w http.ResponseWriter, r *http.Request) {
		//	设置响应结构html
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// 设置响应状态码
		w.WriteHeader(http.StatusOK)
		// 设置响应内容"                  align-items: center;\n            height: 100vh;\n        }\n\n        .container {\n            background-color: #fff;\n            border-radius: 5px;\n            padding: 20px;\n            box-shadow: 0 2px 5px rgba(0, 0, 0, 0.3);\n        }\n\n        h1 {\n            text-align: center;\n            color: #333;\n        }\n\n        form {\n            text-align: center;\n            margin-top: 20px;\n        }\n\n        input[type=\"file\"] {\n            display: none;\n        }\n\n        .custom-file-upload {\n            display: inline-block;\n            padding: 10px 20px;\n            cursor: pointer;\n            background-color: #4CAF50;\n            color: #fff;\n            border-radius: 4px;\n            border: none;\n            transition: background-color 0.3s ease;\n        }\n\n        .custom-file-upload:hover {\n            background-color: #45a049;\n        }\n    </style>\n</head>\n<body>\n<div class=\"container\">\n    <h1>文件上传</h1>\n    <form action=\"/upload\" name=\"fail\" method=\"post\" enctype=\"multipart/form-data\">\n        <label for=\"file-upload\" class=\"custom-file-upload\">\n            <i class=\"fa fa-cloud-upload\"></i> 选择文件\n        </label>\n        <input id=\"file-upload\" type=\"file\" name=\"file\" multiple required>\n        <br><br>\n        <input type=\"submit\" value=\"上传\">\n    </form>\n</div>\n</body>\n</html>\n")
		_, err := w.Write([]byte("<!DOCTYPE html>\n<html>\n  <head>\n    <meta charset=\"utf-8\" />\n    <title>💗</title>\n \n    <style>\n      html,\n      body {\n        height: 100%;\n        padding: 0;\n        margin: 0;\n        background: #000;\n      }\n      canvas {\n        position: absolute;\n        width: 100%;\n        height: 100%;\n        animation: anim 1.5s ease-in-out infinite;\n        -webkit-animation: anim 1.5s ease-in-out infinite;\n        -o-animation: anim 1.5s ease-in-out infinite;\n        -moz-animation: anim 1.5s ease-in-out infinite;\n      }\n      #name {\n        position: absolute;\n        top: 50%;\n        left: 50%;\n        transform: translate(-50%, -50%);\n        margin-top: -20px;\n        font-size: 46px;\n        color: #ea80b0;\n      }\n      @keyframes anim {\n        0% {\n          transform: scale(0.8);\n        }\n        25% {\n          transform: scale(0.7);\n        }\n        50% {\n          transform: scale(1);\n        }\n        75% {\n          transform: scale(0.7);\n        }\n        100% {\n          transform: scale(0.8);\n        }\n      }\n      @-webkit-keyframes anim {\n        0% {\n          -webkit-transform: scale(0.8);\n        }\n        25% {\n          -webkit-transform: scale(0.7);\n        }\n        50% {\n          -webkit-transform: scale(1);\n        }\n        75% {\n          -webkit-transform: scale(0.7);\n        }\n        100% {\n          -webkit-transform: scale(0.8);\n        }\n      }\n      @-o-keyframes anim {\n        0% {\n          -o-transform: scale(0.8);\n        }\n        25% {\n          -o-transform: scale(0.7);\n        }\n        50% {\n          -o-transform: scale(1);\n        }\n        75% {\n          -o-transform: scale(0.7);\n        }\n        100% {\n          -o-transform: scale(0.8);\n        }\n      }\n      @-moz-keyframes anim {\n        0% {\n          -moz-transform: scale(0.8);\n        }\n        25% {\n          -moz-transform: scale(0.7);\n        }\n        50% {\n          -moz-transform: scale(1);\n        }\n        75% {\n          -moz-transform: scale(0.7);\n        }\n        100% {\n          -moz-transform: scale(0.8);\n        }\n      }\n    </style>\n  </head>\n  <body>\n    <canvas id=\"pinkboard\"></canvas>\n    <!-- 在下面加名字 -->\n     <div id=\"name\" style=\"color: blue;\"></div> \n \n    <script>\n      var settings = {\n        particles: {\n          length: 500, \n          duration: 2, \n          velocity: 100, \n          effect: -0.75,\n          size: 30, \n        },\n      };\n      (function () {\n        var b = 0;\n        var c = [\"ms\", \"moz\", \"webkit\", \"o\"];\n        for (var a = 0; a < c.length && !window.requestAnimationFrame; ++a) {\n          window.requestAnimationFrame = window[c[a] + \"RequestAnimationFrame\"];\n          window.cancelAnimationFrame =\n            window[c[a] + \"CancelAnimationFrame\"] ||\n            window[c[a] + \"CancelRequestAnimationFrame\"];\n        }\n        if (!window.requestAnimationFrame) {\n          window.requestAnimationFrame = function (h, e) {\n            var d = new Date().getTime();\n            var f = Math.max(0, 16 - (d - b));\n            var g = window.setTimeout(function () {\n              h(d + f);\n            }, f);\n            b = d + f;\n            return g;\n          };\n        }\n        if (!window.cancelAnimationFrame) {\n          window.cancelAnimationFrame = function (d) {\n            clearTimeout(d);\n          };\n        }\n      })();\n      var Point = (function () {\n        function Point(x, y) {\n          this.x = typeof x !== \"undefined\" ? x : 0;\n          this.y = typeof y !== \"undefined\" ? y : 0;\n        }\n        Point.prototype.clone = function () {\n          return new Point(this.x, this.y);\n        };\n        Point.prototype.length = function (length) {\n          if (typeof length == \"undefined\")\n            return Math.sqrt(this.x * this.x + this.y * this.y);\n          this.normalize();\n          this.x *= length;\n          this.y *= length;\n          return this;\n        };\n        Point.prototype.normalize = function () {\n          var length = this.length();\n          this.x /= length;\n          this.y /= length;\n          return this;\n        };\n        return Point;\n      })();\n      var Particle = (function () {\n        function Particle() {\n          this.position = new Point();\n          this.velocity = new Point();\n          this.acceleration = new Point();\n          this.age = 0;\n        }\n        Particle.prototype.initialize = function (x, y, dx, dy) {\n          this.position.x = x;\n          this.position.y = y;\n          this.velocity.x = dx;\n          this.velocity.y = dy;\n          this.acceleration.x = dx * settings.particles.effect;\n          this.acceleration.y = dy * settings.particles.effect;\n          this.age = 0;\n        };\n        Particle.prototype.update = function (deltaTime) {\n          this.position.x += this.velocity.x * deltaTime;\n          this.position.y += this.velocity.y * deltaTime;\n          this.velocity.x += this.acceleration.x * deltaTime;\n          this.velocity.y += this.acceleration.y * deltaTime;\n          this.age += deltaTime;\n        };\n        Particle.prototype.draw = function (context, image) {\n          function ease(t) {\n            return --t * t * t + 1;\n          }\n          var size = image.width * ease(this.age / settings.particles.duration);\n          context.globalAlpha = 1 - this.age / settings.particles.duration;\n          context.drawImage(\n            image,\n            this.position.x - size / 2,\n            this.position.y - size / 2,\n            size,\n            size\n          );\n        };\n        return Particle;\n      })();\n      var ParticlePool = (function () {\n        var particles,\n          firstActive = 0,\n          firstFree = 0,\n          duration = settings.particles.duration;\n \n        function ParticlePool(length) {\n          particles = new Array(length);\n          for (var i = 0; i < particles.length; i++)\n            particles[i] = new Particle();\n        }\n        ParticlePool.prototype.add = function (x, y, dx, dy) {\n          particles[firstFree].initialize(x, y, dx, dy);\n          firstFree++;\n          if (firstFree == particles.length) firstFree = 0;\n          if (firstActive == firstFree) firstActive++;\n          if (firstActive == particles.length) firstActive = 0;\n        };\n        ParticlePool.prototype.update = function (deltaTime) {\n          var i;\n          if (firstActive < firstFree) {\n            for (i = firstActive; i < firstFree; i++)\n              particles[i].update(deltaTime);\n          }\n          if (firstFree < firstActive) {\n            for (i = firstActive; i < particles.length; i++)\n              particles[i].update(deltaTime);\n            for (i = 0; i < firstFree; i++) particles[i].update(deltaTime);\n          }\n          while (\n            particles[firstActive].age >= duration &&\n            firstActive != firstFree\n          ) {\n            firstActive++;\n            if (firstActive == particles.length) firstActive = 0;\n          }\n        };\n        ParticlePool.prototype.draw = function (context, image) {\n          if (firstActive < firstFree) {\n            for (i = firstActive; i < firstFree; i++)\n              particles[i].draw(context, image);\n          }\n          if (firstFree < firstActive) {\n            for (i = firstActive; i < particles.length; i++)\n              particles[i].draw(context, image);\n            for (i = 0; i < firstFree; i++) particles[i].draw(context, image);\n          }\n        };\n        return ParticlePool;\n      })();\n      (function (canvas) {\n        var context = canvas.getContext(\"2d\"),\n          particles = new ParticlePool(settings.particles.length),\n          particleRate =\n            settings.particles.length / settings.particles.duration, \n          time;\n        function pointOnHeart(t) {\n          return new Point(\n            160 * Math.pow(Math.sin(t), 3),\n            130 * Math.cos(t) -\n              50 * Math.cos(2 * t) -\n              20 * Math.cos(3 * t) -\n              10 * Math.cos(4 * t) +\n              25\n          );\n        }\n        var image = (function () {\n          var canvas = document.createElement(\"canvas\"),\n            context = canvas.getContext(\"2d\");\n          canvas.width = settings.particles.size;\n          canvas.height = settings.particles.size;\n          function to(t) {\n            var point = pointOnHeart(t);\n            point.x =\n              settings.particles.size / 2 +\n              (point.x * settings.particles.size) / 350;\n            point.y =\n              settings.particles.size / 2 -\n              (point.y * settings.particles.size) / 350;\n            return point;\n          }\n          context.beginPath();\n          var t = -Math.PI;\n          var point = to(t);\n          context.moveTo(point.x, point.y);\n          while (t < Math.PI) {\n            t += 0.01;\n            point = to(t);\n            context.lineTo(point.x, point.y);\n          }\n          context.closePath();\n          context.fillStyle = \"#ea80b0\";\n          context.fill();\n          var image = new Image();\n          image.src = canvas.toDataURL();\n          return image;\n        })();\n        function render() {\n          requestAnimationFrame(render);\n          var newTime = new Date().getTime() / 1000,\n            deltaTime = newTime - (time || newTime);\n          time = newTime;\n          context.clearRect(0, 0, canvas.width, canvas.height);\n          var amount = particleRate * deltaTime;\n          for (var i = 0; i < amount; i++) {\n            var pos = pointOnHeart(Math.PI - 2 * Math.PI * Math.random());\n            var dir = pos.clone().length(settings.particles.velocity);\n            particles.add(\n              canvas.width / 2 + pos.x,\n              canvas.height / 2 - pos.y,\n              dir.x,\n              -dir.y\n            );\n          }\n          particles.update(deltaTime);\n          particles.draw(context, image);\n        }\n        function onResize() {\n          canvas.width = canvas.clientWidth;\n          canvas.height = canvas.clientHeight;\n        }\n        window.onresize = onResize;\n        setTimeout(function () {\n          onResize();\n          render();\n        }, 10);\n      })(document.getElementById(\"pinkboard\"));\n \n    </script>\n  </body>\n</html>"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	http.HandleFunc("/ip", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("ip 为", r.RemoteAddr, r.URL.Host)
		w.WriteHeader(http.StatusOK)
		//这里获取的是ip地址将端口去掉
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			fmt.Println(err)
			return
		}
		// 将字符串转化为ip
		ipAddr := net.ParseIP(ip)
		if ipAddr == nil {
			fmt.Println("Invalid IP address")
			return
		}

		// 获取当前时间
		currentTime := time.Now()

		// 获取或初始化 IP 访问记录
		accessRecord, exists := ipAccessMap[ip]
		if !exists {
			accessRecord = struct {
				count          int
				lastAccessTime time.Time
			}{count: 0, lastAccessTime: currentTime}
		}
		//加锁
		mutex.Lock()
		// 检查是否在一分钟内访问超过 100 次
		if currentTime.Sub(accessRecord.lastAccessTime) < time.Minute && accessRecord.count >= 100 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, err = io.WriteString(w, "Too many requests")
			if err != nil {
				return
			}
			return
		}
		// 更新访问次数和最后访问时间
		accessRecord.count++
		accessRecord.lastAccessTime = currentTime
		ipAccessMap[ip] = accessRecord
		mutex.Unlock()
		file, err := os.OpenFile(path.Join(paths, "ip.txt"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			log.Println("打开失败！" + err.Error())
			return
		}
		defer file.Close()
		_, err = file.Write([]byte(ipAddr.String()))
		if err != nil {
			log.Println("ip写入失败", err)
			_, err = io.WriteString(w, "ip获取失败")
			if err != nil {
				return
			}
			return
		}
		_, err = w.Write([]byte("ip" + ipAddr.To16().String()))
		if err != nil {
			return
		}
	})

	http.HandleFunc("/christmastree", func(w http.ResponseWriter, r *http.Request) {
		//	设置响应结构html
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// 设置响应状态码
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("<!DOCTYPE html>\n<html lang=\"en\">\n\n<head>\n    <meta charset=\"UTF-8\">\n\n    <title>Musical Christmas Lights</title>\n\n    <link rel=\"stylesheet\" href=\"https://cdnjs.cloudflare.com/ajax/libs/normalize/5.0.0/normalize.min.css\">\n\n    <style>\n        * {\n            box-sizing: border-box;\n        }\n\n        body {\n            margin: 0;\n            height: 100vh;\n            overflow: hidden;\n            display: flex;\n            align-items: center;\n            justify-content: center;\n            background: #161616;\n            color: #c5a880;\n            font-family: sans-serif;\n        }\n\n        label {\n            display: inline-block;\n            background-color: #161616;\n            padding: 16px;\n            border-radius: 0.3rem;\n            cursor: pointer;\n            margin-top: 1rem;\n            width: 300px;\n            border-radius: 10px;\n            border: 1px solid #c5a880;\n            text-align: center;\n        }\n\n        ul {\n            list-style-type: none;\n            padding: 0;\n            margin: 0;\n        }\n\n        .btn {\n            background-color: #161616;\n            border-radius: 10px;\n            color: #c5a880;\n            border: 1px solid #c5a880;\n            padding: 16px;\n            width: 300px;\n            margin-bottom: 16px;\n            line-height: 1.5;\n            cursor: pointer;\n        }\n\n        .separator {\n            font-weight: bold;\n            text-align: center;\n            width: 300px;\n            margin: 16px 0px;\n            color: #a07676;\n        }\n\n        .title {\n            color: #a07676;\n            font-weight: bold;\n            font-size: 1.25rem;\n            margin-bottom: 16px;\n        }\n\n        .text-loading {\n            font-size: 2rem;\n        }\n    </style>\n\n    <script>\n        window.console = window.console || function (t) {\n        };\n    </script>\n\n\n    <script>\n        if (document.location.search.match(/type=embed/gi)) {\n            window.parent.postMessage(\"resize\", \"*\");\n        }\n    </script>\n\n\n</head>\n\n<body translate=\"no\">\n<script src=\"https://cdn.jsdelivr.net/npm/three@0.115.0/build/three.min.js\"></script>\n<script src=\"https://cdn.jsdelivr.net/npm/three@0.115.0/examples/js/postprocessing/EffectComposer.js\"></script>\n<script src=\"https://cdn.jsdelivr.net/npm/three@0.115.0/examples/js/postprocessing/RenderPass.js\"></script>\n<script src=\"https://cdn.jsdelivr.net/npm/three@0.115.0/examples/js/postprocessing/ShaderPass.js\"></script>\n<script src=\"https://cdn.jsdelivr.net/npm/three@0.115.0/examples/js/shaders/CopyShader.js\"></script>\n<script src=\"https://cdn.jsdelivr.net/npm/three@0.115.0/examples/js/shaders/LuminosityHighPassShader.js\"></script>\n<script src=\"https://cdn.jsdelivr.net/npm/three@0.115.0/examples/js/postprocessing/UnrealBloomPass.js\"></script>\n\n<div id=\"overlay\">\n    <ul>\n        <li class=\"title\">请选择音乐</li>\n        <li>\n            <button class=\"btn\" id=\"btnA\" type=\"button\">\n                Snowflakes Falling Down by Simon Panrucker\n            </button>\n        </li>\n        <li>\n            <button class=\"btn\" id=\"btnB\" type=\"button\">This Christmas by Dott</button>\n        </li>\n        <li>\n            <button class=\"btn\" id=\"btnC\" type=\"button\">No room at the inn by TRG Banks</button>\n        </li>\n        <li>\n            <button class=\"btn\" id=\"btnD\" type=\"button\">Jingle Bell Swing by Mark Smeby</button>\n        </li>\n        <li class=\"separator\">或者</li>\n        <li>\n            <input type=\"file\" id=\"upload\" hidden/>\n            <label for=\"upload\">file</label>\n        </li>\n    </ul>\n</div>\n\n<script id=\"rendered-js\">\n    const {PI, sin, cos} = Math;\n    const TAU = 2 * PI;\n\n    const map = (value, sMin, sMax, dMin, dMax) => {\n        return dMin + (value - sMin) / (sMax - sMin) * (dMax - dMin);\n    };\n\n    const range = (n, m = 0) =>\n        Array(n).fill(m).map((i, j) => i + j);\n\n    const rand = (max, min = 0) => min + Math.random() * (max - min);\n    const randInt = (max, min = 0) => Math.floor(min + Math.random() * (max - min));\n    const randChoise = arr => arr[randInt(arr.length)];\n    const polar = (ang, r = 1) => [r * cos(ang), r * sin(ang)];\n\n    let scene, camera, renderer, analyser;\n    let step = 0;\n    const uniforms = {\n        time: {type: \"f\", value: 0.0},\n        step: {type: \"f\", value: 0.0}\n    };\n\n    const params = {\n        exposure: 1,\n        bloomStrength: 0.9,\n        bloomThreshold: 0,\n        bloomRadius: 0.5\n    };\n\n    let composer;\n\n    const fftSize = 2048;\n    const totalPoints = 4000;\n\n    const listener = new THREE.AudioListener();\n\n    const audio = new THREE.Audio(listener);\n\n    document.querySelector(\"input\").addEventListener(\"change\", uploadAudio, false);\n\n    const buttons = document.querySelectorAll(\".btn\");\n    buttons.forEach((button, index) =>\n        button.addEventListener(\"click\", () => loadAudio(index)));\n\n\n    function init() {\n        const overlay = document.getElementById(\"overlay\");\n        overlay.remove();\n\n        scene = new THREE.Scene();\n        renderer = new THREE.WebGLRenderer({antialias: true});\n        renderer.setPixelRatio(window.devicePixelRatio);\n        renderer.setSize(window.innerWidth, window.innerHeight);\n        document.body.appendChild(renderer.domElement);\n\n        camera = new THREE.PerspectiveCamera(\n            60,\n            window.innerWidth / window.innerHeight,\n            1,\n            1000);\n\n        camera.position.set(-0.09397456774197047, -2.5597086635726947, 24.420789670889008);\n        camera.rotation.set(0.10443543723052419, -0.003827152981119352, 0.0004011488708739715);\n\n        const format = renderer.capabilities.isWebGL2 ?\n            THREE.RedFormat :\n            THREE.LuminanceFormat;\n\n        uniforms.tAudioData = {\n            value: new THREE.DataTexture(analyser.data, fftSize / 2, 1, format)\n        };\n\n\n        addPlane(scene, uniforms, 3000);\n        addSnow(scene, uniforms);\n\n        range(10).map(i => {\n            addTree(scene, uniforms, totalPoints, [20, 0, -20 * i]);\n            addTree(scene, uniforms, totalPoints, [-20, 0, -20 * i]);\n        });\n\n        const renderScene = new THREE.RenderPass(scene, camera);\n\n        const bloomPass = new THREE.UnrealBloomPass(\n            new THREE.Vector2(window.innerWidth, window.innerHeight),\n            1.5,\n            0.4,\n            0.85);\n\n        bloomPass.threshold = params.bloomThreshold;\n        bloomPass.strength = params.bloomStrength;\n        bloomPass.radius = params.bloomRadius;\n\n        composer = new THREE.EffectComposer(renderer);\n        composer.addPass(renderScene);\n        composer.addPass(bloomPass);\n\n        addListners(camera, renderer, composer);\n        animate();\n    }\n\n    function animate(time) {\n        analyser.getFrequencyData();\n        uniforms.tAudioData.value.needsUpdate = true;\n        step = (step + 1) % 1000;\n        uniforms.time.value = time;\n        uniforms.step.value = step;\n        composer.render();\n        requestAnimationFrame(animate);\n    }\n\n\n    function loadAudio(i) {\n        document.getElementById(\"overlay\").innerHTML =\n            '<div class=\"text-loading\">等一下哈 马上来啦...</div>';\n        const files = [\n            \"http://music.163.com/song/media/outer/url?id=1381755293.mp3\",\n            \"http://music.163.com/song/media/outer/url?id=2599493925.mp3\",\n            \"http://music.163.com/song/media/outer/url?id=1991436080.mp3\",\n            \"http://music.163.com/song/media/outer/url?id=306434.mp3\"];\n\n        const file = files[i];\n\n        const loader = new THREE.AudioLoader();\n        loader.load(file, function (buffer) {\n            audio.setBuffer(buffer);\n            audio.play();\n            analyser = new THREE.AudioAnalyser(audio, fftSize);\n            init();\n        });\n\n\n    }\n\n\n        function uploadAudio(event) {\n            document.getElementById(\"overlay\").innerHTML =\n                '<div class=\"text-loading\">等一下哈 马上来啦...</div>';\n            const files = event.target.files;\n            const reader = new FileReader();\n\n            reader.onload = function (file) {\n                var arrayBuffer = file.target.result;\n\n                listener.context.decodeAudioData(arrayBuffer, function (audioBuffer) {\n                    audio.setBuffer(audioBuffer);\n                    audio.play();\n                    analyser = new THREE.AudioAnalyser(audio, fftSize);\n                    init();\n                });\n            };\n\n            reader.readAsArrayBuffer(files[0]);\n        }\n\n    function addTree(scene, uniforms, totalPoints, treePosition) {\n        const vertexShader = `\n      attribute float mIndex;\n      varying vec3 vColor;\n      varying float opacity;\n      uniform sampler2D tAudioData;\n      float norm(float value, float min, float max ){\n       return (value - min) / (max - min);\n      }\n      float lerp(float norm, float min, float max){\n       return (max - min) * norm + min;\n      }\n      float map(float value, float sourceMin, float sourceMax, float destMin, float destMax){\n       return lerp(norm(value, sourceMin, sourceMax), destMin, destMax);\n      }\n      void main() {\n       vColor = color;\n       vec3 p = position;\n       vec4 mvPosition = modelViewMatrix * vec4( p, 1.0 );\n       float amplitude = texture2D( tAudioData, vec2( mIndex, 0.1 ) ).r;\n       float amplitudeClamped = clamp(amplitude-0.4,0.0, 0.6 );\n       float sizeMapped = map(amplitudeClamped, 0.0, 0.6, 1.0, 20.0);\n       opacity = map(mvPosition.z , -200.0, 15.0, 0.0, 1.0);\n       gl_PointSize = sizeMapped * ( 100.0 / -mvPosition.z );\n       gl_Position = projectionMatrix * mvPosition;\n      }\n      `;\n        const fragmentShader = `\n      varying vec3 vColor;\n      varying float opacity;\n      uniform sampler2D pointTexture;\n      void main() {\n       gl_FragColor = vec4( vColor, opacity );\n       gl_FragColor = gl_FragColor * texture2D( pointTexture, gl_PointCoord );\n      }\n      `;\n        const shaderMaterial = new THREE.ShaderMaterial({\n            uniforms: {\n                ...uniforms,\n                pointTexture: {\n                    value: new THREE.TextureLoader().load(`https://assets.codepen.io/3685267/spark1.png`)\n                }\n            },\n\n\n            vertexShader,\n            fragmentShader,\n            blending: THREE.AdditiveBlending,\n            depthTest: false,\n            transparent: true,\n            vertexColors: true\n        });\n\n\n        const geometry = new THREE.BufferGeometry();\n        const positions = [];\n        const colors = [];\n        const sizes = [];\n        const phases = [];\n        const mIndexs = [];\n\n        const color = new THREE.Color();\n\n        for (let i = 0; i < totalPoints; i++) {\n            const t = Math.random();\n            const y = map(t, 0, 1, -8, 10);\n            const ang = map(t, 0, 1, 0, 6 * TAU) + TAU / 2 * (i % 2);\n            const [z, x] = polar(ang, map(t, 0, 1, 5, 0));\n\n            const modifier = map(t, 0, 1, 1, 0);\n            positions.push(x + rand(-0.3 * modifier, 0.3 * modifier));\n            positions.push(y + rand(-0.3 * modifier, 0.3 * modifier));\n            positions.push(z + rand(-0.3 * modifier, 0.3 * modifier));\n\n            color.setHSL(map(i, 0, totalPoints, 1.0, 0.0), 1.0, 0.5);\n\n            colors.push(color.r, color.g, color.b);\n            phases.push(rand(1000));\n            sizes.push(1);\n            const mIndex = map(i, 0, totalPoints, 1.0, 0.0);\n            mIndexs.push(mIndex);\n        }\n\n        geometry.setAttribute(\n            \"position\",\n            new THREE.Float32BufferAttribute(positions, 3).setUsage(\n                THREE.DynamicDrawUsage));\n\n\n        geometry.setAttribute(\"color\", new THREE.Float32BufferAttribute(colors, 3));\n        geometry.setAttribute(\"size\", new THREE.Float32BufferAttribute(sizes, 1));\n        geometry.setAttribute(\"phase\", new THREE.Float32BufferAttribute(phases, 1));\n        geometry.setAttribute(\"mIndex\", new THREE.Float32BufferAttribute(mIndexs, 1));\n\n        const tree = new THREE.Points(geometry, shaderMaterial);\n\n        const [px, py, pz] = treePosition;\n\n        tree.position.x = px;\n        tree.position.y = py;\n        tree.position.z = pz;\n\n        scene.add(tree);\n    }\n\n    function addSnow(scene, uniforms) {\n        const vertexShader = `\n      attribute float size;\n      attribute float phase;\n      attribute float phaseSecondary;\n      varying vec3 vColor;\n      varying float opacity;\n      uniform float time;\n      uniform float step;\n      float norm(float value, float min, float max ){\n       return (value - min) / (max - min);\n      }\n      float lerp(float norm, float min, float max){\n       return (max - min) * norm + min;\n      }\n      float map(float value, float sourceMin, float sourceMax, float destMin, float destMax){\n       return lerp(norm(value, sourceMin, sourceMax), destMin, destMax);\n      }\n      void main() {\n       float t = time* 0.0006;\n       vColor = color;\n       vec3 p = position;\n       p.y = map(mod(phase+step, 1000.0), 0.0, 1000.0, 25.0, -8.0);\n       p.x += sin(t+phase);\n       p.z += sin(t+phaseSecondary);\n       opacity = map(p.z, -150.0, 15.0, 0.0, 1.0);\n       vec4 mvPosition = modelViewMatrix * vec4( p, 1.0 );\n       gl_PointSize = size * ( 100.0 / -mvPosition.z );\n       gl_Position = projectionMatrix * mvPosition;\n      }\n      `;\n\n        const fragmentShader = `\n      uniform sampler2D pointTexture;\n      varying vec3 vColor;\n      varying float opacity;\n      void main() {\n       gl_FragColor = vec4( vColor, opacity );\n       gl_FragColor = gl_FragColor * texture2D( pointTexture, gl_PointCoord );\n      }\n      `;\n\n        function createSnowSet(sprite) {\n            const totalPoints = 300;\n            const shaderMaterial = new THREE.ShaderMaterial({\n                uniforms: {\n                    ...uniforms,\n                    pointTexture: {\n                        value: new THREE.TextureLoader().load(sprite)\n                    }\n                },\n\n\n                vertexShader,\n                fragmentShader,\n                blending: THREE.AdditiveBlending,\n                depthTest: false,\n                transparent: true,\n                vertexColors: true\n            });\n\n\n            const geometry = new THREE.BufferGeometry();\n            const positions = [];\n            const colors = [];\n            const sizes = [];\n            const phases = [];\n            const phaseSecondaries = [];\n\n            const color = new THREE.Color();\n\n            for (let i = 0; i < totalPoints; i++) {\n                const [x, y, z] = [rand(25, -25), 0, rand(15, -150)];\n                positions.push(x);\n                positions.push(y);\n                positions.push(z);\n\n                color.set(randChoise([\"#f1d4d4\", \"#f1f6f9\", \"#eeeeee\", \"#f1f1e8\"]));\n\n                colors.push(color.r, color.g, color.b);\n                phases.push(rand(1000));\n                phaseSecondaries.push(rand(1000));\n                sizes.push(rand(4, 2));\n            }\n\n            geometry.setAttribute(\n                \"position\",\n                new THREE.Float32BufferAttribute(positions, 3));\n\n            geometry.setAttribute(\"color\", new THREE.Float32BufferAttribute(colors, 3));\n            geometry.setAttribute(\"size\", new THREE.Float32BufferAttribute(sizes, 1));\n            geometry.setAttribute(\"phase\", new THREE.Float32BufferAttribute(phases, 1));\n            geometry.setAttribute(\n                \"phaseSecondary\",\n                new THREE.Float32BufferAttribute(phaseSecondaries, 1));\n\n\n            const mesh = new THREE.Points(geometry, shaderMaterial);\n\n            scene.add(mesh);\n        }\n\n        const sprites = [\n            \"https://assets.codepen.io/3685267/snowflake1.png\",\n            \"https://assets.codepen.io/3685267/snowflake2.png\",\n            \"https://assets.codepen.io/3685267/snowflake3.png\",\n            \"https://assets.codepen.io/3685267/snowflake4.png\",\n            \"https://assets.codepen.io/3685267/snowflake5.png\"];\n\n        sprites.forEach(sprite => {\n            createSnowSet(sprite);\n        });\n    }\n\n    function addPlane(scene, uniforms, totalPoints) {\n        const vertexShader = `\n      attribute float size;\n      attribute vec3 customColor;\n      varying vec3 vColor;\n      void main() {\n       vColor = customColor;\n       vec4 mvPosition = modelViewMatrix * vec4( position, 1.0 );\n       gl_PointSize = size * ( 300.0 / -mvPosition.z );\n       gl_Position = projectionMatrix * mvPosition;\n      }\n      `;\n        const fragmentShader = `\n      uniform vec3 color;\n      uniform sampler2D pointTexture;\n      varying vec3 vColor;\n      void main() {\n       gl_FragColor = vec4( vColor, 1.0 );\n       gl_FragColor = gl_FragColor * texture2D( pointTexture, gl_PointCoord );\n      }\n      `;\n        const shaderMaterial = new THREE.ShaderMaterial({\n            uniforms: {\n                ...uniforms,\n                pointTexture: {\n                    value: new THREE.TextureLoader().load(`https://assets.codepen.io/3685267/spark1.png`)\n                }\n            },\n\n\n            vertexShader,\n            fragmentShader,\n            blending: THREE.AdditiveBlending,\n            depthTest: false,\n            transparent: true,\n            vertexColors: true\n        });\n\n\n        const geometry = new THREE.BufferGeometry();\n        const positions = [];\n        const colors = [];\n        const sizes = [];\n\n        const color = new THREE.Color();\n\n        for (let i = 0; i < totalPoints; i++) {\n            const [x, y, z] = [rand(-25, 25), 0, rand(-150, 15)];\n            positions.push(x);\n            positions.push(y);\n            positions.push(z);\n\n            color.set(randChoise([\"#93abd3\", \"#f2f4c0\", \"#9ddfd3\"]));\n\n            colors.push(color.r, color.g, color.b);\n            sizes.push(1);\n        }\n\n        geometry.setAttribute(\n            \"position\",\n            new THREE.Float32BufferAttribute(positions, 3).setUsage(\n                THREE.DynamicDrawUsage));\n\n\n        geometry.setAttribute(\n            \"customColor\",\n            new THREE.Float32BufferAttribute(colors, 3));\n\n        geometry.setAttribute(\"size\", new THREE.Float32BufferAttribute(sizes, 1));\n\n        const plane = new THREE.Points(geometry, shaderMaterial);\n\n        plane.position.y = -8;\n        scene.add(plane);\n    }\n\n    function addListners(camera, renderer, composer) {\n        document.addEventListener(\"keydown\", e => {\n            const {x, y, z} = camera.position;\n            console.log(`camera.position.set(${x},${y},${z})`);\n            const {x: a, y: b, z: c} = camera.rotation;\n            console.log(`camera.rotation.set(${a},${b},${c})`);\n        });\n\n        window.addEventListener(\n            \"resize\",\n            () => {\n                const width = window.innerWidth;\n                const height = window.innerHeight;\n\n                camera.aspect = width / height;\n                camera.updateProjectionMatrix();\n\n                renderer.setSize(width, height);\n                composer.setSize(width, height);\n            },\n            false);\n\n    }\n</script>\n\n</body>\n\n</html>"))
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	})
	http.HandleFunc("/i", func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			fmt.Println(err)
			return
		}

		ipAddr := net.ParseIP(ip)
		if ipAddr == nil {
			fmt.Println("Invalid IP address")
			return
		}

		if ipAddr.To4() != nil {
			fmt.Println("IPv4:", ipAddr)
		} else if ipAddr.To16() != nil {
			fmt.Println("IPv6:", ipAddr)
		} else {
			fmt.Println("Unknown IP address")
		}
		file, err := os.Open("4.mp4")
		if err != nil {
			log.Println("打开失败" + err.Error())
		}
		defer file.Close()
		_, err = io.Copy(w, file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	/*分片上传*/
	http.HandleFunc("/uploads", uploadHandler)
	http.HandleFunc("/merge", mergeHandler)
	http.HandleFunc("/history", historyHandler)
	http.HandleFunc("/", indexHandler)

	log.Println("web server is star")
	err := http.ListenAndServe(addr, nil)
	//err := http.ListenAndServeTLS(addr, "/root/ssl/xlei.fun.pem", "/root/ssl/xlei.fun.key", nil)
	if err != nil {
		log.Println(err)
	}
}
