// Copyright 2021-present The Atlas Authors. All rights reserved.
// This source code is licensed under the Apache 2.0 license found
// in the LICENSE file in the root directory of this source tree.

package schemahcl

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestAttributes(t *testing.T) {
	f := `i  = 1
b  = true
s  = "hello, world"
sl = ["hello", "world"]
bl = [true, false]
hd = <<-EOT
  hello
  world
EOT
vars = {
  a = "a"
}
`
	var test struct {
		Int        int                  `spec:"i"`
		Bool       bool                 `spec:"b"`
		Str        string               `spec:"s"`
		StringList []string             `spec:"sl"`
		BoolList   []bool               `spec:"bl"`
		Heredoc    string               `spec:"hd"`
		Vars       map[string]cty.Value `spec:"vars"`
	}
	err := New().EvalBytes([]byte(f), &test, nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, test.Int)
	require.EqualValues(t, true, test.Bool)
	require.EqualValues(t, "hello, world", test.Str)
	require.EqualValues(t, []string{"hello", "world"}, test.StringList)
	require.EqualValues(t, []bool{true, false}, test.BoolList)
	require.EqualValues(t, "hello\nworld\n", test.Heredoc)
	require.EqualValues(t, "a", test.Vars["a"].AsString())
	// Heredoc needs to be explicitly formatted this way.
	test.Heredoc = "<<-EOT\n  hello\n  world\nEOT"
	marshal, err := Marshal(&test)
	require.NoError(t, err)
	require.EqualValues(t, f, string(marshal))

	var v struct {
		NullV string  `spec:"null_v"`
		NullP *string `spec:"null_p"`
	}
	err = New().EvalBytes([]byte(`
null_v = null
null_p = null
`), &v, nil)
	require.NoError(t, err)
	require.Empty(t, v.NullV)
	require.Nil(t, v.NullP)
}

func TestResource(t *testing.T) {
	f := `endpoint "/hello" {
  description = "the hello handler"
  timeout_ms  = 100
  handler {
    active = true
    addr   = ":8080"
    tag {
      name  = "name"
      value = "value"
    }
  }
}
`
	type (
		Handler struct {
			Active bool   `spec:"active"`
			Addr   string `spec:"addr"`
			Tag    struct {
				Name  string `spec:"name"`
				Value string `spec:"value"`
			} `spec:"tag"`
		}
		Endpoint struct {
			Name        string   `spec:",name"`
			Description string   `spec:"description"`
			TimeoutMs   int      `spec:"timeout_ms"`
			Handler     *Handler `spec:"handler"`
		}
		File struct {
			Endpoints []*Endpoint `spec:"endpoint"`
		}
	)
	var test File
	err := New().EvalBytes([]byte(f), &test, nil)
	require.NoError(t, err)
	require.Len(t, test.Endpoints, 1)
	expected := &Endpoint{
		Name:        "/hello",
		Description: "the hello handler",
		TimeoutMs:   100,
		Handler: &Handler{
			Active: true,
			Addr:   ":8080",
			Tag: struct {
				Name  string `spec:"name"`
				Value string `spec:"value"`
			}{Name: "name", Value: "value"},
		},
	}
	require.EqualValues(t, expected, test.Endpoints[0])
	buf, err := Marshal(&test)
	require.NoError(t, err)
	require.EqualValues(t, f, string(buf))
}

func TestInvalidRefs(t *testing.T) {
	var doc struct {
		Tables []struct {
			Name string `spec:",name"`
			Refs []*Ref `spec:"ref"`
		}
	}
	err := New().EvalBytes([]byte(`
table "bar" {
  refs = [table]
}
`), &doc, nil)
	require.EqualError(t, err, ":3,3-17: invalid reference used in refs")
}

func ExampleUnmarshal() {
	f := `
show "seinfeld" {
	day = SUN
	writer "jerry" {
		full_name = "Jerry Seinfeld"	
	}
	writer "larry" {
		full_name = "Larry David"	
	}
}`

	type (
		Writer struct {
			ID       string `spec:",name"`
			FullName string `spec:"full_name"`
		}
		Show struct {
			Name    string    `spec:",name"`
			Day     string    `spec:"day"`
			Writers []*Writer `spec:"writer"`
		}
	)
	var (
		test struct {
			Shows []*Show `spec:"show"`
		}
		opts = []Option{
			WithScopedEnums("show.day", "SUN", "MON", "TUE"),
		}
	)
	err := New(opts...).EvalBytes([]byte(f), &test, nil)
	if err != nil {
		panic(err)
	}
	seinfeld := test.Shows[0]
	fmt.Printf("the show %q at day %s has %d writers.", seinfeld.Name, seinfeld.Day, len(seinfeld.Writers))
	// Output: the show "seinfeld" at day SUN has 2 writers.
}

func ExampleMarshal() {
	type (
		Point struct {
			ID string `spec:",name"`
			X  int    `spec:"x"`
			Y  int    `spec:"y"`
		}
	)
	var test = struct {
		Points []*Point `spec:"point"`
	}{
		Points: []*Point{
			{ID: "start", X: 0, Y: 0},
			{ID: "end", X: 1, Y: 1},
		},
	}
	b, err := Marshal(&test)
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Println(string(b))
	// Output:
	// point "start" {
	//   x = 0
	//   y = 0
	// }
	// point "end" {
	//   x = 1
	//   y = 1
	// }
}

func TestIgnore(t *testing.T) {
	var test struct {
		IgnoreAttr    string `spec:"-"`
		NotIgnoreAttr string `spec:"not_ignore_attr"`
		IgnoreBlock   struct {
			Name string `spec:",name"`
		} `spec:"-"`
		NotIgnore struct {
			Name string `spec:",name"`
		} `spec:"not_ignore_block"`
	}
	test.IgnoreAttr, test.NotIgnoreAttr = "ignore", "not ignore"
	test.IgnoreBlock.Name, test.NotIgnore.Name = "ignore", "not ignore"
	buf, err := Marshal(&test)
	require.NoError(t, err)
	require.Equal(t, `not_ignore_attr = "not ignore"
not_ignore_block "not ignore" {
}
`, string(buf))
}

func TestInterface(t *testing.T) {
	type (
		Animal interface {
			animal()
		}
		Parrot struct {
			Animal
			Name string `spec:",name"`
			Boss string `spec:"boss"`
		}
		Lion struct {
			Animal
			Name   string `spec:",name"`
			Friend string `spec:"friend"`
		}
		Zoo struct {
			Animals []Animal `spec:""`
		}
		Cast struct {
			Animal Animal `spec:""`
		}
	)
	Register("lion", &Lion{})
	Register("parrot", &Parrot{})
	t.Run("single", func(t *testing.T) {
		f := `
cast "lion_king" {
	lion "simba" {
		friend = "rafiki"
	}
}
`
		var test struct {
			Cast *Cast `spec:"cast"`
		}
		err := New().EvalBytes([]byte(f), &test, nil)
		require.NoError(t, err)
		require.EqualValues(t, &Cast{
			Animal: &Lion{
				Name:   "simba",
				Friend: "rafiki",
			},
		}, test.Cast)
	})
	t.Run("slice", func(t *testing.T) {
		f := `
zoo "ramat_gan" {
	lion "simba" {
		friend = "rafiki"
	}
	parrot "iago" {
		boss = "jafar"
	}
}
`
		var test struct {
			Zoo *Zoo `spec:"zoo"`
		}
		err := New().EvalBytes([]byte(f), &test, nil)
		require.NoError(t, err)
		require.EqualValues(t, &Zoo{
			Animals: []Animal{
				&Lion{
					Name:   "simba",
					Friend: "rafiki",
				},
				&Parrot{
					Name: "iago",
					Boss: "jafar",
				},
			},
		}, test.Zoo)
	})
}

func TestQualified(t *testing.T) {
	type Person struct {
		Name  string `spec:",name"`
		Title string `spec:",qualifier"`
	}
	var test struct {
		Person *Person `spec:"person"`
	}
	h := `person "dr" "jekyll" {
}
`
	err := New().EvalBytes([]byte(h), &test, nil)
	require.NoError(t, err)
	require.EqualValues(t, test.Person, &Person{
		Title: "dr",
		Name:  "jekyll",
	})
	out, err := Marshal(&test)
	require.NoError(t, err)
	require.EqualValues(t, h, string(out))
}

func TestNameAttr(t *testing.T) {
	h := `
named "block_id" {
  name = "atlas"
}
ref = named.block_id.name
`
	type Named struct {
		Name string `spec:"name,name"`
	}
	var test struct {
		Named *Named `spec:"named"`
		Ref   string `spec:"ref"`
	}
	err := New().EvalBytes([]byte(h), &test, nil)
	require.NoError(t, err)
	require.EqualValues(t, &Named{
		Name: "atlas",
	}, test.Named)
	require.EqualValues(t, "atlas", test.Ref)
}

func TestRefPatch(t *testing.T) {
	type (
		Family struct {
			Name string `spec:"name,name"`
		}
		Person struct {
			Name   string `spec:",name"`
			Family *Ref   `spec:"family"`
		}
	)
	Register("family", &Family{})
	Register("person", &Person{})
	var test struct {
		Families []*Family `spec:"family"`
		People   []*Person `spec:"person"`
	}
	h := `
variable "family_name" {
  type = string
}

family "default" {
	name = var.family_name
}

person "rotem" {
	family = family.default
}
`
	err := New().EvalBytes([]byte(h), &test, map[string]cty.Value{
		"family_name": cty.StringVal("tam"),
	})
	require.NoError(t, err)
	require.EqualValues(t, "$family.tam", test.People[0].Family.V)
}

func TestMultiFile(t *testing.T) {
	type Person struct {
		Name   string `spec:",name"`
		Hobby  string `spec:"hobby"`
		Parent *Ref   `spec:"parent"`
	}
	var test struct {
		People []*Person `spec:"person"`
	}
	var (
		paths   []string
		testDir = "testdata/"
	)
	dir, err := os.ReadDir(testDir)
	require.NoError(t, err)
	for _, file := range dir {
		if file.IsDir() {
			continue
		}
		paths = append(paths, filepath.Join(testDir, file.Name()))
	}
	err = New().EvalFiles(paths, &test, map[string]cty.Value{
		"hobby": cty.StringVal("coding"),
	})
	require.NoError(t, err)
	require.Len(t, test.People, 2)
	require.EqualValues(t, &Person{Name: "rotemtam", Hobby: "coding"}, test.People[0])
	require.EqualValues(t, &Person{
		Name:   "tzuri",
		Hobby:  "ice-cream",
		Parent: &Ref{V: "$person.rotemtam"},
	}, test.People[1])
}

func TestForEachResources(t *testing.T) {
	type (
		Env struct {
			Name string `spec:",name"`
			URL  string `spec:"url"`
		}
	)
	var (
		doc struct {
			Envs []*Env `spec:"env"`
		}
		b = []byte(`
variable "tenants" {
  type    = list(string)
  default = ["atlas", "ent"]
}

variable "domains" {
  type = list(object({
    name = string
    port = number
  }))
  default = [
    {
      name = "atlasgo.io"
      port = 443
    },
    {
      name = "entgo.io"
      port = 443
    },
  ]
}

env "prod" {
  for_each = toset(var.tenants)
  url = "mysql://root:pass@:3306/${each.value}"
}

env "staging" {
  for_each = toset(var.domains)
  url = "${each.value.name}:${each.value.port}"
  driver = MYSQL
}

env "dev" {
  for_each = {
    atlas = "atlasgo.io"
    ent   = "entgo.io"
  }
  url = "${each.value}/${each.key}"
}
`)
	)
	require.NoError(t, New(
		WithScopedEnums("env.driver", "MYSQL", "POSTGRES"),
		WithDataSource("sql", func(_ context.Context, ectx *hcl.EvalContext, b *hclsyntax.Block) (cty.Value, error) {
			attrs, diags := b.Body.JustAttributes()
			if diags.HasErrors() {
				return cty.NilVal, diags
			}
			v, diags := attrs["query"].Expr.Value(ectx)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}
			return cty.ObjectVal(map[string]cty.Value{"query": v}), nil
		}),
	).EvalBytes(b, &doc, nil))
	require.Len(t, doc.Envs, 6)
	require.Equal(t, "prod", doc.Envs[0].Name)
	require.EqualValues(t, doc.Envs[0].URL, "mysql://root:pass@:3306/atlas")
	require.Equal(t, "prod", doc.Envs[1].Name)
	require.EqualValues(t, doc.Envs[1].URL, "mysql://root:pass@:3306/ent")
	require.Equal(t, "staging", doc.Envs[2].Name)
	require.EqualValues(t, doc.Envs[2].URL, "atlasgo.io:443")
	require.Equal(t, "staging", doc.Envs[3].Name)
	require.EqualValues(t, doc.Envs[3].URL, "entgo.io:443")
	require.Equal(t, "dev", doc.Envs[4].Name)
	require.EqualValues(t, doc.Envs[4].URL, "atlasgo.io/atlas")
	require.Equal(t, "dev", doc.Envs[5].Name)
	require.EqualValues(t, doc.Envs[5].URL, "entgo.io/ent")

	// Mismatched element types.
	err := New().EvalBytes(b, &doc, map[string]cty.Value{
		"domains": cty.ListVal([]cty.Value{
			cty.ObjectVal(map[string]cty.Value{
				"name": cty.StringVal("a"),
				"port": cty.StringVal("b"),
			}),
		}),
	})
	require.EqualError(t, err, `variable "domains": a number is required`)

	var (
		// For-each resource depends on other resources.
		doc1 struct {
			Schema []*struct {
				Name string `spec:",name"`
			} `spec:"schema"`
			Table []*struct {
				Name   string `spec:"name,name"`
				Schema *Ref   `spec:"schema"`
			} `spec:"table"`
		}
		b1 = []byte(`
schema "s1" {}
schema "s2" {}

table {
  for_each = {
    t1 = schema.s1
	t2 = schema.s2
  }
  name = each.key
  schema = each.value
}
`)
	)
	err = New().EvalBytes(b1, &doc1, nil)
	require.NoError(t, err)
	buf, err := Marshal.MarshalSpec(&doc1)
	require.NoError(t, err)
	require.Equal(t, `schema "s1" {
}
schema "s2" {
}
table "t1" {
  schema = schema.s1
}
table "t2" {
  schema = schema.s2
}
`, string(buf))

	// Tuple of type any.
	b1 = []byte(`
schema "s1" {
  comment = "schema comment"
}
schema "s2" {
  # object without comment.
}

table {
  for_each = [schema.s1, schema.s2]
  name = each.value.name
  schema = each.value
}
`)
	err = New().EvalBytes(b1, &doc1, nil)
	require.NoError(t, err)
	buf, err = Marshal.MarshalSpec(&doc1)
	require.NoError(t, err)
	require.Equal(t, `schema "s1" {
}
schema "s2" {
}
table "s1" {
  schema = schema.s1
}
table "s2" {
  schema = schema.s2
}
`, string(buf))
}

func TestDataLocalsRefs(t *testing.T) {
	var (
		opts = []Option{
			WithDataSource("sql", func(_ context.Context, ectx *hcl.EvalContext, b *hclsyntax.Block) (cty.Value, error) {
				attrs, diags := b.Body.JustAttributes()
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				v, diags := attrs["result"].Expr.Value(ectx)
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				return cty.ObjectVal(map[string]cty.Value{"output": v}), nil
			}),
			WithDataSource("text", func(_ context.Context, ectx *hcl.EvalContext, b *hclsyntax.Block) (cty.Value, error) {
				attrs, diags := b.Body.JustAttributes()
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				v, diags := attrs["value"].Expr.Value(ectx)
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				return cty.ObjectVal(map[string]cty.Value{"output": v}), nil
			}),
			WithInitBlock("atlas", func(_ context.Context, ectx *hcl.EvalContext, b *hclsyntax.Block) (cty.Value, error) {
				org, diags := b.Body.Attributes["org"].Expr.Value(ectx)
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				if len(b.Body.Blocks) != 1 || b.Body.Blocks[0].Type != "auth" {
					return cty.NilVal, errors.New("expected auth block")
				}
				attrs, diags := b.Body.Blocks[0].Body.JustAttributes()
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				host, diags := attrs["host"].Expr.Value(ectx)
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				return cty.ObjectVal(map[string]cty.Value{
					"org": org,
					"auth": cty.ObjectVal(map[string]cty.Value{
						"host": host,
					}),
				}), nil
			}),
			WithDataSource("remote_dir", func(_ context.Context, ectx *hcl.EvalContext, b *hclsyntax.Block) (cty.Value, error) {
				attrs, diags := b.Body.JustAttributes()
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				host, diags := attrs["host"].Expr.Value(ectx)
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				org, diags := (&hclsyntax.ScopeTraversalExpr{
					Traversal: hcl.Traversal{
						hcl.TraverseRoot{Name: "atlas", SrcRange: b.Range()},
						hcl.TraverseAttr{Name: "org", SrcRange: b.Range()},
					},
				}).Value(ectx)
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				return cty.ObjectVal(map[string]cty.Value{
					"url": cty.StringVal("atlas://" + path.Join(host.AsString(), org.AsString(), b.Labels[1])),
				}), nil
			}),
		}
		doc struct {
			Values []string `spec:"vs"`
		}
		b = []byte(`
variable "url" {
  type    = string
  default = "mysql://root:pass@:3306/atlas"
}

locals {
  a = "local-a"
  // locals can reference other locals.
  b = "local-b-ref-local-a: ${local.a}"
  // locals can reference data sources.
  c = "local-c-ref-data-a: ${data.text.a.output}"
  d = "local-d"
  host = "atlasgo.io"
  obj = {
    k = "obj-v"
  }
}

data "sql" "tenants" {
  url = var.url
  // language=mysql
  query = <<EOS
SELECT schema_name
  FROM information_schema.schemata
  WHERE schema_name LIKE 'tenant_%'
EOS
  // fake result.
  result = "data-sql-tenants"
}

data "text" "a" {
  // data sources can reference data sources.
  value = "data-text-a-ref-data-sql-tenants: ${data.sql.tenants.output}"
}

data "text" "b" {
  // data sources can reference locals.
  value = "data-text-b-ref-local-d: ${local.d}"
}

atlas {
  org = "ent"
  auth {
    host = local.host
  }
}

data "remote_dir" "migrations" {
  host = atlas.auth.host
}

data "text" "obj" {
  value = local.obj.k
}
vs = [
  local.a,
  local.b,
  local.c,
  data.sql.tenants.output,
  data.text.a.output,
  data.text.b.output,
  data.remote_dir.migrations.url,
  data.text.obj.output,
]
`)
	)
	require.NoError(t, New(opts...).EvalBytes(b, &doc, nil))
	require.Equal(t, []string{
		"local-a",
		"local-b-ref-local-a: local-a",
		"local-c-ref-data-a: data-text-a-ref-data-sql-tenants: data-sql-tenants",
		"data-sql-tenants",
		"data-text-a-ref-data-sql-tenants: data-sql-tenants",
		"data-text-b-ref-local-d: local-d",
		"atlas://atlasgo.io/ent/migrations",
		"obj-v",
	}, doc.Values)

	b = []byte(`locals { a = local.a }`)
	require.EqualError(t, New(opts...).EvalBytes(b, &doc, nil), `cyclic reference to "local.a"`)

	b = []byte(`
locals {
  a = "a"
  b = local.c
  c = local.b
}
`)
	require.Error(t, New(opts...).EvalBytes(b, &doc, nil), `cyclic reference to "local.c"`)

	b = []byte(`
data "text" "a" {
  value = local.a
}

locals {
  a = data.text.a.output
}
`)
	require.Error(t, New(opts...).EvalBytes(b, &doc, nil), `cyclic reference to "data.text.a"`)

	b = []byte(`
out = data.unknown.a.output
`)
	require.EqualError(t, New(opts...).EvalBytes(b, &doc, nil), `:2,7-11: Unknown data source; data.unknown.a.output does not exist`)
}

func TestSkippedDataSrc(t *testing.T) {
	var (
		opts = []Option{
			WithDataSource("dynamic", func(_ context.Context, ectx *hcl.EvalContext, b *hclsyntax.Block) (cty.Value, error) {
				attrs, diags := b.Body.JustAttributes()
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				s, diags := attrs["skip"].Expr.Value(ectx)
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				if s.True() {
					return cty.NilVal, fmt.Errorf("data source should be skipped, but was called with %q", b.Labels)
				}
				v, diags := attrs["v"].Expr.Value(ectx)
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				return cty.ObjectVal(map[string]cty.Value{
					"v": v,
				}), nil
			}),
		}
		v struct {
			V2 string `spec:"v2"`
			V3 string `spec:"v3"`
		}
		b = []byte(`
data "dynamic" "skipped1" {
  v = "value is irrelevant"
  // This attribute has no meaning, besides indicating
  // to the test that the data source should be skipped.
  skip = true
}
data "dynamic" "skipped2" {
  v = data.dynamic.skipped1.v
  skip = true
}
data "dynamic" "evaluated1" {
  v = "v1"
  skip = false
}
data "dynamic" "evaluated2" {
  v = "v2"
  skip = false
}
data "dynamic" "evaluated3" {
  v = data.dynamic.evaluated1.v
  skip = false
}

v2 = data.dynamic.evaluated2.v
v3 = data.dynamic.evaluated3.v
`)
	)
	require.NoError(t, New(opts...).EvalBytes(b, &v, nil))

	b = []byte(`
locals {
  a = data.dynamic.not_skipped1.v
  b = local.c
  c = "3"
  d = data.dynamic.not_skipped3.v
}

data "dynamic" "not_skipped1" {
  v = "v2"
  skip = false
}

data "dynamic" "not_skipped2" {
  v = "v${local.b}"
  skip = false
}

data "dynamic" "not_skipped3" {
  v = "v4"
  skip = false
}

data "dynamic" "not_skipped4" {
  v = "v5"
  skip = false
}

data "dynamic" "skipped1" {
  v = local.a
  skip = true
}

data "dynamic" "skipped2" {
  v = "v"
  skip = true
}

data "dynamic" "skipped3" {
  v = data.dynamic.skipped2
  skip = true
}

top {
  a = data.dynamic.not_skipped2.v // "v3".
  block {
    block {
      a1 = local.d // "v4".
      a2 = data.dynamic.not_skipped4.v // "v5".
    }
  }
}

v2 = local.a
`)
	var v1 struct {
		Top struct {
			A     string `spec:"a"`
			Block struct {
				Block struct {
					A1 string `spec:"a1"`
					A2 string `spec:"a2"`
				} `spec:"block"`
			} `spec:"block"`
		} `spec:"top"`
		V2 string `spec:"v2"`
	}
	require.NoError(t, New(opts...).EvalBytes(b, &v1, nil))
	require.Equal(t, "v2", v1.V2)
	require.Equal(t, "v3", v1.Top.A)
	require.Equal(t, "v4", v1.Top.Block.Block.A1)
	require.Equal(t, "v5", v1.Top.Block.Block.A2)
}

func TestTypeLabelBlock(t *testing.T) {
	var (
		callD, callT int
		opts         = []Option{
			WithTypeLabelBlock("driver", "remote", func(_ context.Context, ectx *hcl.EvalContext, b *hclsyntax.Block) (cty.Value, error) {
				attrs, diags := b.Body.JustAttributes()
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				v, diags := attrs["name"].Expr.Value(ectx)
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				callT++
				return cty.ObjectVal(map[string]cty.Value{"url": cty.StringVal("driver://" + v.AsString())}), nil
			}),
			WithTypeLabelBlock("driver", "not_called", func(context.Context, *hcl.EvalContext, *hclsyntax.Block) (cty.Value, error) {
				t.Fatal("should not be called")
				return cty.NilVal, nil
			}),
			WithDataSource("text", func(_ context.Context, ectx *hcl.EvalContext, b *hclsyntax.Block) (cty.Value, error) {
				attrs, diags := b.Body.JustAttributes()
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				v, diags := attrs["value"].Expr.Value(ectx)
				if diags.HasErrors() {
					return cty.NilVal, diags
				}
				callD++
				return cty.ObjectVal(map[string]cty.Value{"output": v}), nil
			}),
		}
		doc struct {
			Values []string `spec:"vs"`
		}
		b = []byte(`
locals {
  a = "a8m"
}

data "text" "a" {
  value = local.a
}

driver "remote" "myapp" {
  name = data.text.a.output
}

vs = [
  driver.remote.myapp.url,
  data.text.a.output
]
`)
	)
	require.NoError(t, New(opts...).EvalBytes(b, &doc, nil))
	require.Equal(t, []string{"driver://a8m", "a8m"}, doc.Values)
	require.EqualValues(t, 1, callT)
	require.EqualValues(t, 2, callD, "it is up to the data source to implement caching")

	b = []byte(`
locals {
  a = "a8m"
}

data "text" "a" {
  value = local.a
}

driver "remote" "myapp" {
  name = data.text.a.output
}
`)
	require.NoError(t, New(opts...).EvalBytes(b, &doc, nil))
	require.Equal(t, []string{"driver://a8m", "a8m"}, doc.Values)
	require.Equal(t, 1, callT)
	require.Equal(t, 2, callD)
}

type countValidator struct{ nb, na int }

func (*countValidator) Err() error { return nil }
func (c *countValidator) ValidateBody(*hcl.EvalContext, *hclsyntax.Body) (func() error, error) {
	return func() error { return nil }, nil
}
func (c *countValidator) ValidateBlock(*hcl.EvalContext, *hclsyntax.Block) (func() error, error) {
	c.nb++
	return func() error { return nil }, nil
}
func (c *countValidator) ValidateAttribute(*hcl.EvalContext, *hclsyntax.Attribute, cty.Value) error {
	c.na++
	return nil
}

func TestSchemaValidator(t *testing.T) {
	var (
		cv  = &countValidator{}
		doc struct {
			DefaultExtension
		}
	)
	err := New(
		WithSchemaValidator(func() SchemaValidator {
			return cv
		}),
	).EvalBytes([]byte(`
block "a" {}
block "b" {}
block "c" {}
attr1 = "a"
attr2 = "b"
`), &doc, nil)
	require.NoError(t, err)
	require.Equal(t, 3, cv.nb)
	require.Equal(t, 2, cv.na)
}

func Test_ExtraReferences(t *testing.T) {
	type (
		Foo struct {
			Name      string `spec:",name"`
			Qualifier string `spec:",qualifier"`
			DefaultExtension
		}
		Block1 struct {
			Name string `spec:",name"`
			Type string `spec:"type"`
		}
		Block2 struct {
			Name string `spec:",name"`
			Refs []*Ref `spec:"refs"`
		}
		ExtraBar struct {
			Blk1 *Block1 `spec:"blk1"`
			Blk2 *Block2 `spec:"blk2"`
		}
	)
	var (
		doc struct {
			Foo []*Foo `spec:"foo"`
		}
		b = []byte(`
foo "f1" {
	extra_bar {
		blk1 "c1" {
			type = "c1"
		}
		blk2 "p1" {
			refs = [blk1.c1]
		}
	}
}
`)
	)
	require.NoError(t, New().EvalBytes(b, &doc, nil))
	require.Len(t, doc.Foo, 1)

	extra, ok := doc.Foo[0].Extra.Resource("extra_bar")
	require.True(t, ok)
	var e ExtraBar
	require.NoError(t, extra.As(&e))
	require.Equal(t, &ExtraBar{
		Blk1: &Block1{
			Name: "c1",
			Type: "c1",
		},
		Blk2: &Block2{
			Name: "p1",
			Refs: []*Ref{{V: "$blk1.c1"}},
		},
	}, &e)
}

func Test_ScopeContextOverride(t *testing.T) {
	type (
		Foo struct {
			Name string `spec:",name"`
		}
		Bar struct {
			Name string `spec:",name"`
			Attr string `spec:"attr"`
			Ref  *Ref   `spec:"ref"`
		}
	)
	var (
		doc struct {
			Foo []*Foo `spec:"foo"`
			Bar []*Bar `spec:"bar"`
		}
		b = []byte(`
foo "f1" {}
bar "b1" {
	attr = foo
	ref  = foo.f1
}
`)
	)
	require.NoError(t, New(
		WithScopedEnums("bar.attr", "foo"), // foo is a valid value for bar.attr.
	).EvalBytes(b, &doc, nil))
	require.Len(t, doc.Foo, 1)
	require.Len(t, doc.Bar, 1)
	require.Equal(t, &Bar{
		Name: "b1",
		Attr: "foo",
		Ref:  &Ref{V: "$foo.f1"},
	}, doc.Bar[0])
}

func Test_MarshalAttr(t *testing.T) {
	var doc struct {
		DefaultExtension
	}
	doc.Extra.Attrs = append(
		doc.Extra.Attrs,
		StringEnumsAttr("mixed1", &EnumString{S: "string"}, &EnumString{E: "enum"}),
		StringEnumsAttr("mixed2", &EnumString{E: "enum1"}, &EnumString{E: "enum2"}),
		StringEnumsAttr("mixed3", &EnumString{S: "string1"}, &EnumString{S: "string1"}),
	)
	buf, err := Marshal(&doc)
	require.NoError(t, err)
	require.Equal(t, `mixed1 = ["string", enum]
mixed2 = [enum1, enum2]
mixed3 = ["string1", "string1"]
`, string(buf))
}

func Test_WithPos(t *testing.T) {
	var (
		doc struct {
			Bar struct {
				A int `spec:"a"`
				DefaultExtension
			} `spec:"bar"`
			Qux struct {
				Range *hcl.Range `spec:",range"`
				A     int        `spec:"a"`
				DefaultExtension
			} `spec:"qux"`
			DefaultExtension
		}
		b = []byte(`
foo {}
foo {
  bar {
    baz = 1
  }
}
baz = 1
bar {
  a = 1
}
qux {
  a = 1
}
`)
	)
	require.NoError(t, New(WithPos()).EvalBytes(b, &doc, nil))
	at, ok := doc.Extra.Attr("baz")
	require.True(t, ok)
	require.Equal(t, 8, at.Range().Start.Line)
	rs := doc.Extra.Resources("foo")
	require.Len(t, rs, 2)
	require.Equal(t, 2, rs[0].Range().Start.Line)
	require.Equal(t, 3, rs[1].Range().Start.Line)
	require.Equal(t, 4, rs[1].Children[0].Range().Start.Line)
	require.Equal(t, 5, rs[1].Children[0].Attrs[0].Range().Start.Line)
	require.NotNil(t, doc.Bar.Extra.Range(), "position should be attached to the resource")
	require.Equal(t, doc.Qux.Range.Start.Line, 12)
	require.Nil(t, doc.Qux.Extra.Range(), "position should not be attached if it was explicitly set")
}

func TestExtendedBlockDef(t *testing.T) {
	var (
		doc struct {
			DefaultExtension
		}
		b = []byte(`
schema "public" {}
table "users" {}
materialized "users_view2" {
  schema = schema.public
  as = "SELECT * FROM script_matview_inspect.users"
  column "id" {
    null = false
  }
  column "a" {
    null = false
  }
  column "b" {
    null = false
  }
  primary_key {
    columns = [column.id]
  }
  populate = true
}
materialized "users_view" {
  schema = schema.public
  to = table.users
  as = "SELECT * FROM script_matview_inspect.users"
  index "i" {
    on {
      expr = "a"
    }
  }
  primary_key {
    using = index.i # Not a real syntax.
  }
}
`)
	)
	require.NoError(t, New().EvalBytes(b, &doc, nil))
}

func TestUseTraversal(t *testing.T) {
	for _, tt := range []struct {
		x    string
		want bool
	}{
		{
			x:    "atlas.env",
			want: true,
		},
		{
			x: "\"atlas.env\"",
		},
		{
			x:    "\"${atlas.env}\"",
			want: true,
		},
		{
			x:    "contains([], atlas.env)",
			want: true,
		},
		{
			x:    "contains([], 1) ? atlas.env : 1",
			want: true,
		},
		{
			x: "contains([], 1) ? atlas.dev : 1",
		},
	} {
		pr := hclparse.NewParser()
		f, diags := pr.ParseHCL([]byte(fmt.Sprintf("x = %s", tt.x)), "test.hcl")
		require.False(t, diags.HasErrors())
		x := f.Body.(*hclsyntax.Body).Attributes["x"]
		require.False(t, diags.HasErrors())
		got := UseTraversal(x.Expr, hcl.Traversal{
			hcl.TraverseRoot{Name: "atlas"},
			hcl.TraverseAttr{Name: "env"},
		})
		require.Equal(t, tt.want, got)
	}
}

func TestEscapeHeredoc(t *testing.T) {
	type (
		Attr struct {
			K string `spec:",name"`
			V string `spec:"value"`
		}
		Block struct {
			Attrs []*Attr `spec:"attr"`
		}
		doc struct {
			Blocks []*Block `spec:"block"`
		}
	)
	v := &doc{
		Blocks: []*Block{
			{
				Attrs: []*Attr{
					{
						K: "inline",
						V: "Hello ${username}, welcome! %{ if true }you're in%{ endif }",
					},
					{
						K: "multiline",
						V: `<<-TEXT
 Hello ${username}, welcome! %{ if true }you're in%{ endif }
 ${{text}}, ${{ text }}, ${{- text -}}
 $${{text}}, %%%{text}
TEXT`,
					},
				},
			},
		},
	}
	buf, err := Marshal(v)
	require.NoError(t, err)
	require.Equal(t, `block {
  attr "inline" {
    value = "Hello $${username}, welcome! %%{ if true }you're in%%{ endif }"
  }
  attr "multiline" {
    value = <<-TEXT
 Hello $${username}, welcome! %%{ if true }you're in%%{ endif }
 $${{text}}, $${{ text }}, $${{- text -}}
 $$${{text}}, %%%%{text}
TEXT
  }
}
`, string(buf))
	var got doc
	require.NoError(t, New().EvalBytes(buf, &got, nil))
	require.Equal(t,
		"Hello ${username}, welcome! %{ if true }you're in%{ endif }",
		got.Blocks[0].Attrs[0].V,
	)
	require.Equal(t,
		`Hello ${username}, welcome! %{ if true }you're in%{ endif }
${{text}}, ${{ text }}, ${{- text -}}
$${{text}}, %%%{text}
`,
		got.Blocks[0].Attrs[1].V,
	)
}
