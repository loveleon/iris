package main

import "github.com/kataras/iris/v12"

func main() {
	e := iris.Django(nil, ".html") // You can still use a file system though.
	e.AddFunc("greet", func(name string) string {
		return "Hello, " + name + "!"
	})
	err := e.ParseTemplate("program.html", []byte(`<h1>{{greet(Name)}}</h1>`))
	if err != nil {
		panic(err)
	}

	app := iris.New()
	app.RegisterView(e)
	app.Get("/", index)

	app.Listen(":8080")
}

func index(ctx iris.Context) {
	ctx.View("program.html", iris.Map{
		"Name": "Gerasimos",
	})
}
