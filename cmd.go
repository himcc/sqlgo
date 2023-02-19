package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/glebarez/sqlite"
	"github.com/mkideal/cli"
	"gorm.io/gorm"
)

type Params struct {
	cli.Helper

	Separator   string `cli:"s,sep" usage:"column separator. default is \\t" dft:"\t"`
	DB          string `cli:"d,database" usage:"The path of database file. The file will be created when it does not exist. \n\t\t\t\tTo retain the db file, please provide a path, otherwise, the db in memory will be used." dft:":memory:"`
	TableDefine string `cli:"t,table" usage:"Table define format '[tableName]:[colName1] coltype1,[colName2] coltype2 ...'. \n\t\t\t\tDefault table name is _t. Default col name is _num(num starts from 1). \n\t\t\t\tDefault col type is text."`
	SQL         string `cli:"*e,exec" usage:"SQL to be executed。"`
}

type ColDefine struct {
	ColName string
	ColType string
}

type TableDefine struct {
	TableName string
	Cols      []ColDefine
}

func cmd() {
	os.Exit(cli.Run(new(Params), func(ctx *cli.Context) error {
		params := ctx.Argv().(*Params)

		// fmt.Println(*params)

		//open db
		db, err := gorm.Open(sqlite.Open(params.DB), &gorm.Config{})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)

			d, err2 := db.DB()
			if err2 != nil {
				fmt.Fprintln(os.Stderr, err2)
			}
			err3 := d.Close()
			if err3 != nil {
				fmt.Fprintln(os.Stderr, err3)
			}

			return err
		}
		openedDB = db
		defer func() {
			if d, err := openedDB.DB(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			} else {
				if err2 := d.Close(); err2 != nil {
					fmt.Fprintln(os.Stderr, err)
				}
			}
		}()

		// parse table define
		td := parseTableDefine(params.TableDefine)
		if td.TableName == "" {
			td.TableName = "_t"
		}
		if td.Cols != nil {
			createTable(td)
		}

		//
		r := csv.NewReader(os.Stdin)
		sp := []rune(params.Separator)[0]
		r.Comma = sp
		var rowNum = 0
		for {
			record, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Fatal(err)
			}
			rowNum++
			if rowNum == 1 && td.Cols == nil {
				var cols []ColDefine
				for i, _ := range record {
					cols = append(cols, ColDefine{"_" + strconv.Itoa(i+1), "text"})
				}
				td.Cols = cols
				createTable(td)
			}
			var insertSQL = "insert into `" + td.TableName + "` values ("
			for i, colDefine := range td.Cols {
				if i != 0 {
					insertSQL += ", "
				}
				if colDefine.ColType == "text" {
					insertSQL = insertSQL + "'" + record[i] + "'"
				} else {
					insertSQL = insertSQL + record[i]
				}
			}
			insertSQL += ")"
			_, err2 := runRetNum(insertSQL, "insert")
			if err2 != nil {
				log.Fatalln(err2)
			}
		}

		// query
		h, err2 := runSelect(params.SQL)
		if err2 != nil {
			log.Fatalln(err2)
		}
		cols := h["cols"].([]string)
		rows := h["rows"].([][]any)

		for i, n := range cols {
			if i != 0 {
				fmt.Print(string(sp))
			}
			fmt.Print(n)
		}
		fmt.Print("\n")

		for _, row := range rows {
			for i, col := range row {
				if i != 0 {
					fmt.Print(string(sp))
				}
				fmt.Print(*col.(*any))
			}
			fmt.Print("\n")
		}

		return nil
	}))
}

func parseTableDefine(t string) TableDefine {
	var ret TableDefine
	if strings.Trim(t, " ") == "" {
		return ret
	}
	if strings.IndexByte(t, ':') >= 0 {
		items := strings.Split(t, ":")
		if len(items) == 2 {
			if strings.HasPrefix(t, ":") {
				ret.Cols = parseCols(items[1])
			} else if strings.HasSuffix(t, ":") {
				ret.TableName = items[0]
			} else {
				ret.TableName = items[0]
				ret.Cols = parseCols(items[1])
			}
		} else {
			log.Fatalln("table define err A")
		}

	} else {
		if strings.IndexByte(t, ' ') < 0 {
			ret.TableName = t
		} else {
			ret.Cols = parseCols(t)
		}
	}
	return ret
}

func parseCols(t string) []ColDefine {
	var ret []ColDefine
	if strings.Trim(t, " ") == "" {
		return nil
	} else {
		items := strings.Split(t, ",")
		for i, item := range items {
			if strings.Trim(item, " ") == "" {
				log.Fatalln("table define err B")
			}
			if strings.IndexByte(item, ' ') >= 0 {
				xx := strings.Split(item, " ")
				if len(xx) == 2 {
					if strings.HasPrefix(item, " ") {
						ret = append(ret, ColDefine{"_" + strconv.Itoa(i+1), xx[1]})
					} else if strings.HasSuffix(item, " ") {
						ret = append(ret, ColDefine{xx[0], "text"})
					} else {
						ret = append(ret, ColDefine{xx[0], xx[1]})
					}
				} else {
					log.Fatalln("table define err C")
				}
			} else {
				log.Fatalln("table define err D")
			}
		}
	}
	return ret
}

func createTable(td TableDefine) {
	var colDefine []string
	for _, nameType := range td.Cols {
		colDefine = append(colDefine, " `"+nameType.ColName+"` "+nameType.ColType+" ")
	}
	var createTableSQL = "create table `" + td.TableName + "` ( " + strings.Join(colDefine, ", ") + " )"

	_, err3 := runRetNum(createTableSQL, "createTable")
	if err3 != nil {
		log.Fatalln(err3)
	}
}
