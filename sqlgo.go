package main

import (
	"embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shirou/gopsutil/disk"
	"github.com/xuri/excelize/v2"
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
	if len(os.Args) > 1 {
		cmd()
	} else {
		wenbUI()
	}
}

func wenbUI() {

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

	r.GET("/selectdatafile", func(c *gin.Context) {
		c.HTML(200, "selectdatafile.html", nil)
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
		sqlstr = strings.ReplaceAll(strings.ReplaceAll(sqlstr, "\r", " "), "\n", " ")
		sqlstr = strings.Trim(sqlstr, " ")
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

	r.GET("/parsefile", func(c *gin.Context) {
		f := c.Query("f")
		f = strings.Trim(f, " \n\r")
		if len(f) < 1 {
			c.HTML(200, "parsefile.html", gin.H{
				"err": "file path is blank",
			})
			return
		}
		//  err
		//  name
		//  cont[]
		//    name
		//    rows[][]
		fItems := strings.Split(f, ".")
		extName := strings.ToLower(fItems[len(fItems)-1])
		if extName == "csv" {
			osfile, err := os.Open(f)
			if err != nil {
				c.HTML(200, "parsefile.html", gin.H{
					"err": f + " " + err.Error(),
				})
				return
			}
			r := csv.NewReader(osfile)
			var records [][]string
			var num = 0
			for {
				record, err := r.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					c.HTML(200, "parsefile.html", gin.H{
						"err": f + " " + err.Error(),
					})
					return
				}
				records = append(records, record)
				num++
				if num >= 10 {
					break
				}
			}
			err3 := osfile.Close()
			if err3 != nil {
				log.Println(f, err3)
			}
			var cont []gin.H
			cont = append(cont, gin.H{
				"name": "",
				"rows": records,
			})
			c.HTML(200, "parsefile.html", gin.H{
				"name": f,
				"cont": cont,
			})
			return
		} else if extName == "xlsx" {
			cont, err := getExcelTop(f)
			if err != nil {
				c.HTML(200, "parsefile.html", gin.H{
					"err": "file path is blank",
				})
				return
			}
			c.HTML(200, "parsefile.html", gin.H{
				"name": f,
				"cont": cont,
			})
			return
		} else {
			c.HTML(200, "parsefile.html", gin.H{
				"err": f + " file format don't support",
			})
			return
		}
	})

	r.POST("/loadfile", func(c *gin.Context) {
		type LoadFileParams struct {
			Sheetno   int
			Filepath  string
			HasHeader bool
			Tablename string
			Cols      []map[string]string
		}
		params, _ := c.GetPostForm("params")

		var p LoadFileParams
		err := json.Unmarshal([]byte(params), &p)
		if err != nil {
			log.Println(err)
			c.JSON(200, gin.H{
				"err": err.Error(),
			})
			return
		}
		// create table
		var colDefine []string
		for _, nameType := range p.Cols {
			colDefine = append(colDefine, " `"+nameType["colname"]+"` "+nameType["coltype"]+" ")
		}
		var createTableSQL = "create table `" + p.Tablename + "` ( " + strings.Join(colDefine, ", ") + " )"
		log.Println(createTableSQL)
		_, err3 := runRetNum(createTableSQL, "createTable")
		if err3 != nil {
			c.JSON(200, gin.H{
				"err": err3.Error(),
			})
			return
		}

		// load data
		fItems := strings.Split(p.Filepath, ".")
		extName := strings.ToLower(fItems[len(fItems)-1])
		var fn func() ([]string, error)
		if extName == "csv" {
			fn, err = getCsvReader(p.Filepath)
			if err != nil {
				c.JSON(200, gin.H{
					"err": err.Error(),
				})
				return
			}
		} else if extName == "xlsx" {
			fn, err = getExcelReader(p.Filepath, p.Sheetno)
			if err != nil {
				c.JSON(200, gin.H{
					"err": err.Error(),
				})
				return
			}
		} else {
			c.JSON(200, gin.H{
				"err": p.Filepath + " file format don't support",
			})
			return
		}
		var lineno = 0
		for {
			lineno++
			records, err4 := fn()
			if err4 != nil {
				c.JSON(200, gin.H{
					"err": err4.Error(),
				})
				return
			}
			if p.HasHeader && lineno == 1 {
				continue
			}
			if records == nil {
				break
			} else {
				var sql = "insert into `" + p.Tablename + "` values ("
				for i, nameType := range p.Cols {
					if i != 0 {
						sql = sql + ", "
					}
					if nameType["coltype"] == "text" {
						sql = sql + "'" + records[i] + "'"
					} else {
						sql = sql + records[i]
					}
				}
				sql = sql + ")"
				h, err5 := runRetNum(sql, "insert")
				if err5 != nil {
					c.JSON(200, gin.H{
						"err": err5.Error(),
					})
					return
				}
				if h["num"].(int64) != 1 {
					c.JSON(200, gin.H{
						"err": "insert RowsAffected " + strconv.FormatInt(h["num"].(int64), 10),
					})
					return
				}
			}
		}

		c.JSON(200, gin.H{
			"err": nil,
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
		log.Println(err)
		log.Println("\n\n\nOpen browser failed. You can open " + url + " in your browser manually.\n\n\n")
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
		return runRetNum(sql, "create")
	} else if strings.ToLower(words[0]) == "drop" {
		return runRetNum(sql, "drop")
	} else {
		return nil, errors.New("support select , insert , create , drop , delete , update")
	}
}

func getExcelTop(filename string) ([]gin.H, error) {

	f, err := excelize.OpenFile(filename)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, err
	}
	defer func() {
		// Close the spreadsheet.
		if err := f.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	sheetList := f.GetSheetList()

	var ret []gin.H

	for _, sheet := range sheetList {

		rows, err := f.Rows(sheet)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return nil, err
		}
		rowNo := 1
		var myrows [][]string
		for rows.Next() {
			if rowNo > 10 {
				break
			} else {
				rowNo++
			}
			row, err := rows.Columns()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return nil, err
			}
			myrows = append(myrows, row)
		}
		ret = append(ret, gin.H{
			"name": sheet,
			"rows": myrows,
		})
		if err = rows.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	return ret, nil
}

func getCsvReader(filepath string) (func() ([]string, error), error) {
	osfile, err := os.Open(filepath)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	r := csv.NewReader(osfile)
	return func() ([]string, error) {
		record, err := r.Read()
		if err == io.EOF {
			err3 := osfile.Close()
			if err3 != nil {
				return nil, err3
			}
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return record, nil
	}, nil
}

func getExcelReader(filepath string, sheetno int) (func() ([]string, error), error) {
	f, err := excelize.OpenFile(filepath)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	sheetList := f.GetSheetList()
	rows, err := f.Rows(sheetList[sheetno])
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return func() ([]string, error) {
		if rows.Next() {
			row, err := rows.Columns()
			if err != nil {
				log.Println(err)
				return nil, err
			}
			return row, nil
		} else {
			err3 := f.Close()
			if err3 != nil {
				return nil, err3
			}
			return nil, nil
		}
	}, nil
}
