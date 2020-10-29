// Copyright 2020 Liquidata, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package function

import "github.com/dolthub/go-mysql-server/sql"

type ConnectionID struct {
	NoArgFunc
}

func connIDFuncLogic(ctx *sql.Context, _ sql.Row) (interface{}, error) {
	return ctx.ID(), nil
}

var _ sql.FunctionExpression = ConnectionID{}

func NewConnectionID() sql.Expression {
	return ConnectionID{
		NoArgFunc: NoArgFunc{"connection_id", sql.Uint32},
	}
}

// Eval implements sql.Expression
func (c ConnectionID) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	return connIDFuncLogic(ctx, row)
}

// WithChildren implements sql.Expression
func (c ConnectionID) WithChildren(expressions ...sql.Expression) (sql.Expression, error) {
	return NoArgFuncWithChildren(c, expressions)
}

type User struct {
	NoArgFunc
}

func userFuncLogic(ctx *sql.Context, _ sql.Row) (interface{}, error) {
	return ctx.Client().User, nil
}

var _ sql.FunctionExpression = User{}

func NewUser() sql.Expression {
	return User{
		NoArgFunc: NoArgFunc{"user", sql.LongText},
	}
}

func NewCurrentUser() sql.Expression {
	return User{
		NoArgFunc: NoArgFunc{"current_user", sql.LongText},
	}
}

// Eval implements sql.Expression
func (c User) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	return userFuncLogic(ctx, row)
}

// WithChildren implements sql.Expression
func (c User) WithChildren(expressions ...sql.Expression) (sql.Expression, error) {
	return NoArgFuncWithChildren(c, expressions)
}