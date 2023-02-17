package main

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shirou/gopsutil/disk"
	"gorm.io/gorm"
)

type MyFileInfo struct {
	Name  string
	IsDir bool
}

var (
	openedDB     *gorm.DB
	openedDBFile = ""

	//go:embed template/* static/*
	embedfs embed.FS
)

func main() {

	staticFs, err2 := fs.Sub(embedfs, "static")
	if err2 != nil {
		log.Fatalln(err2)
	}

	templateFs, err2 := fs.Sub(embedfs, "template")
	if err2 != nil {
		log.Fatalln(err2)
	}

	r := gin.Default()

	r.StaticFS("/static", http.FS(staticFs))

	templ := template.Must(template.New("").Delims("{{", "}}").Funcs(r.FuncMap).ParseFS(templateFs, "*.html"))
	r.SetHTMLTemplate(templ)

	r.GET("/", func(c *gin.Context) {
		cookieCurrentdb, err := c.Cookie("currentdb")
		if err != nil {
			log.Println(err)
			c.Redirect(302, "/selectdb")
			return
		}
		if cookieCurrentdb == "" {
			c.Redirect(302, "/selectdb")
			return
		}
		cookieCurrentdb, err = url.QueryUnescape(cookieCurrentdb)
		if err != nil {
			log.Println(err)
			c.Redirect(302, "/selectdb")
			return
		}
		if cookieCurrentdb != openedDBFile {
			err := opendb(cookieCurrentdb)
			if err != nil {
				log.Println(err)
				c.Redirect(302, "/selectdb")
				return
			}
		}
		c.HTML(200, "index.html", openedDBFile)
	})

	r.GET("/left", func(c *gin.Context) {

		names, err := getTablesName()
		if err != nil {
			c.HTML(200, "left.html", gin.H{
				"err": err,
			})
			return
		}

		c.HTML(200, "left.html", gin.H{
			"dbfile": openedDBFile,
			"tables": names,
		})
	})

	r.GET("/up", func(c *gin.Context) {

		c.HTML(200, "up.html", nil)
	})

	r.GET("/down", func(c *gin.Context) {

		c.HTML(200, "down.html", nil)
	})

	r.GET("/selectdb", func(c *gin.Context) {

		c.HTML(200, "selectdb.html", nil)
	})

	r.GET("/explorer", func(c *gin.Context) {
		p := c.Query("p")
		if len(p) < 1 {
			omg, _ := os.Getwd()
			p = omg
		}
		fileInfoList, err := ioutil.ReadDir(p)
		if err != nil {
			c.JSON(200, gin.H{
				"code": 0,
				"msg":  err.Error(),
			})
			return
		}
		var fileInfos []MyFileInfo
		for _, f := range fileInfoList {
			fileInfos = append(fileInfos, MyFileInfo{f.Name(), f.IsDir()})
		}
		var allPartitions []string
		if runtime.GOOS == "windows" {
			ps, err := disk.Partitions(true)
			if err != nil {
				c.JSON(200, gin.H{
					"code": 0,
					"msg":  err.Error(),
				})
				return
			}
			for _, p := range ps {
				allPartitions = append(allPartitions, p.Mountpoint)
			}
		}
		c.JSON(200, gin.H{
			"code":          200,
			"path":          p,
			"items":         fileInfos,
			"allPartitions": allPartitions,
		})
	})

	r.GET("/opendb", func(c *gin.Context) {
		p := c.Query("p")
		if len(p) < 1 {
			c.JSON(200, gin.H{
				"code": 0,
				"msg":  "db file path is blank",
			})
			return
		}
		//
		if p != openedDBFile {
			err := opendb(p)
			if err != nil {
				c.JSON(200, gin.H{
					"code": 0,
					"msg":  "db file path is blank",
				})
				return
			}
		}
		//
		c.JSON(200, gin.H{
			"code": 200,
			"msg":  "ok",
		})
	})

	r.GET("/tablemeta", func(c *gin.Context) {
		t := c.Query("t")
		if len(t) < 1 {
			c.HTML(200, "tablemeta.html", gin.H{
				"err": "table name is blank",
			})
			return
		}
		//
		var createTableSQL string
		openedDB.Raw("SELECT sql FROM sqlite_master WHERE type='table' and name= ? ", t).Scan(&createTableSQL)
		sampleData, err := runSelect("select * from " + t + " limit 10")
		if err != nil {
			c.HTML(200, "tablemeta.html", gin.H{
				"err": err,
			})
			return
		}
		//
		c.HTML(200, "tablemeta.html", gin.H{
			"tableName":      t,
			"createTableSQL": createTableSQL,
			"sampleData":     sampleData,
		})
	})

	r.POST("/sqlexec", func(c *gin.Context) {
		sqlstr := c.PostForm("sqlstr")
		sqlstr = strings.Trim(sqlstr, " \n\r")
		if len(sqlstr) < 1 {
			c.HTML(200, "result.html", gin.H{
				"err": "sql is blank",
			})
			return
		}
		sqlstmts := strings.Split(sqlstr, ";")
		if len(sqlstmts) < 1 {
			c.HTML(200, "result.html", gin.H{
				"err": "sql is blank",
			})
			return
		}
		var results []any
		for _, sqlstmt := range sqlstmts {
			sqlstmt = strings.Trim(sqlstmt, " \n\r")
			if sqlstmt == "" {
				continue
			}
			result, err := runSQL(sqlstmt)
			if err != nil {
				results = append(results, gin.H{
					"err": err.Error(),
					"sql": sqlstmt,
				})
			} else {
				results = append(results, gin.H{
					"err":    nil,
					"sql":    sqlstmt,
					"result": result,
				})
			}
		}
		c.HTML(200, "result.html", gin.H{
			"results": results,
		})
	})

	go func() {
		time.Sleep(time.Duration(100) * time.Millisecond)
		openbrowser("http://127.0.0.1:6012")
	}()

	r.Run(":6012")

}

func opendb(dbfile string) error {
	db, err := gorm.Open(sqlite.Open(dbfile), &gorm.Config{})
	if err != nil {
		log.Println(err)

		d, err2 := db.DB()
		if err2 != nil {
			log.Println(err2)
		}
		err3 := d.Close()
		if err3 != nil {
			log.Println(err3)
		}

		return err
	}

	r, err := db.Raw("SELECT name FROM sqlite_master WHERE type=\"table\";").Rows()
	if err != nil {
		log.Println(err)

		d, err2 := db.DB()
		if err2 != nil {
			log.Println(err2)
		}
		err3 := d.Close()
		if err3 != nil {
			log.Println(err3)
		}

		return err
	}
	var names []string
	for r.Next() {
		var n string
		r.Scan(&n)
		names = append(names, n)
	}
	if openedDB != nil {
		d, err2 := openedDB.DB()
		if err2 != nil {
			log.Println(err2)
		}
		err3 := d.Close()
		if err3 != nil {
			log.Println(err3)
		}
	}
	openedDB = db
	openedDBFile = dbfile
	return nil
}

func openbrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		log.Fatal(err)
	}
}

func getTablesName() ([]string, error) {
	r, err := openedDB.Raw("SELECT name FROM sqlite_master WHERE type=\"table\";").Rows()
	if err != nil {
		log.Println(err)
		return nil, err
	}
	var names []string
	for r.Next() {
		var n string
		r.Scan(&n)
		names = append(names, n)
	}
	return names, nil
}

func runRetNum(sql string, sqltype string) (gin.H, error) {
	d := openedDB.Exec(sql)
	err := d.Error
	if err != nil {
		return nil, err
	}
	num := d.RowsAffected
	return gin.H{"num": num, "type": sqltype}, nil
}
func runSelect(sql string) (gin.H, error) {
	r, err := openedDB.Raw(sql).Rows()
	if err != nil {
		log.Println(err)
		return nil, err
	}
	cols, err := r.Columns()
	if err != nil {
		log.Println(err)
		return nil, err
	}
	var myrows [][]any
	for r.Next() {
		var n []any
		for i := 0; i < len(cols); i++ {
			var x any
			n = append(n, &x)
		}
		r.Scan(n...)
		myrows = append(myrows, n)
	}
	return gin.H{"cols": cols, "rows": myrows, "type": "select"}, nil
}

func runSQL(sql string) (gin.H, error) {
	log.Println("run sql : " + sql)
	words := strings.Split(sql, " ")
	if strings.ToLower(words[0]) == "select" {
		return runSelect(sql)
	} else if strings.ToLower(words[0]) == "delete" {
		return runRetNum(sql, "delete")
	} else if strings.ToLower(words[0]) == "update" {
		return runRetNum(sql, "update")
	} else if strings.ToLower(words[0]) == "insert" {
		return runRetNum(sql, "insert")
	} else if strings.ToLower(words[0]) == "create" {
		if len(words) > 2 && (strings.ToLower(words[1]) == "table" || strings.ToLower(words[2]) == "table") {
			return runRetNum(sql, "createTable")
		} else {
			return nil, errors.New("support select , insert , create table , delete , update")
		}
	} else {
		return nil, errors.New("support select , insert , create table , delete , update")
	}
}
