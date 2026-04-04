package parse

import (
	"strings"
	"testing"
)

func TestPythonExtractor(t *testing.T) {
	src := `
import os
from typing import List, Optional

class UserService:
    def __init__(self, db):
        self.db = db

    async def get_user(self, user_id: str) -> Optional[dict]:
        return await self.db.find(user_id)

    def list_users(self) -> List[dict]:
        return self.db.all()

def helper_function(x: int, y: int) -> int:
    return x + y
`
	sk := ExtractSkeleton(src, "service.py")
	if sk == nil {
		t.Fatal("nil skeleton")
	}
	if sk.Language != "python" {
		t.Errorf("lang = %s", sk.Language)
	}
	if len(sk.Imports) < 2 {
		t.Errorf("imports = %d, want >= 2", len(sk.Imports))
	}
	if len(sk.Classes) != 1 || sk.Classes[0].Name != "UserService" {
		t.Errorf("classes = %v", sk.Classes)
	}
	if len(sk.Classes[0].Methods) < 2 {
		t.Errorf("methods = %d, want >= 2", len(sk.Classes[0].Methods))
	}
	if len(sk.Functions) < 1 {
		t.Errorf("functions = %d, want >= 1", len(sk.Functions))
	}
}

func TestJsTsExtractor(t *testing.T) {
	src := `
import { useState } from 'react';
import axios from 'axios';

export class ApiClient extends BaseClient {
    async fetchData(url) {
        return axios.get(url);
    }
}

export function formatDate(date) {
    return date.toISOString();
}

export const multiply = (a, b) => a * b;
`
	sk := ExtractSkeleton(src, "api.ts")
	if sk == nil {
		t.Fatal("nil skeleton")
	}
	if sk.Language != "typescript" {
		t.Errorf("lang = %s", sk.Language)
	}
	if len(sk.Imports) < 2 {
		t.Errorf("imports = %d", len(sk.Imports))
	}
	if len(sk.Classes) != 1 || sk.Classes[0].Name != "ApiClient" {
		t.Errorf("classes = %v", sk.Classes)
	}
	if sk.Classes[0].Extends != "BaseClient" {
		t.Errorf("extends = %s", sk.Classes[0].Extends)
	}
	if len(sk.Functions) < 2 {
		t.Errorf("functions = %d, want >= 2", len(sk.Functions))
	}
}

func TestJavaExtractor(t *testing.T) {
	src := `
package com.example.service;

import java.util.List;
import java.util.Optional;

public class UserController extends BaseController implements Serializable {
    public List<User> getUsers() {
        return userService.findAll();
    }

    public Optional<User> getUser(String id) {
        return userService.findById(id);
    }
}
`
	sk := ExtractSkeleton(src, "UserController.java")
	if sk == nil {
		t.Fatal("nil skeleton")
	}
	if !strings.Contains(sk.Package, "com.example") {
		t.Errorf("package = %s", sk.Package)
	}
	if len(sk.Classes) != 1 || sk.Classes[0].Name != "UserController" {
		t.Errorf("classes = %v", sk.Classes)
	}
	if sk.Classes[0].Extends != "BaseController" {
		t.Errorf("extends = %s", sk.Classes[0].Extends)
	}
}

func TestRustExtractor(t *testing.T) {
	src := `
use std::collections::HashMap;

pub struct Config {
    name: String,
    value: i32,
}

pub trait Configurable {
    fn configure(&self) -> Result<(), Error>;
}

impl Config {
    pub fn new(name: String) -> Self {
        Config { name, value: 0 }
    }
}

pub async fn load_config(path: &str) -> Result<Config, Error> {
    todo!()
}
`
	sk := ExtractSkeleton(src, "config.rs")
	if sk == nil {
		t.Fatal("nil skeleton")
	}
	if len(sk.Classes) < 1 {
		t.Errorf("structs = %d", len(sk.Classes))
	}
	if len(sk.Interfaces) < 1 {
		t.Errorf("traits = %d", len(sk.Interfaces))
	}
	if len(sk.Functions) < 2 {
		t.Errorf("functions = %d", len(sk.Functions))
	}
}

func TestSkeletonToAbstract(t *testing.T) {
	sk := &CodeSkeleton{
		Language: "python",
		Classes: []ClassInfo{
			{Name: "MyClass", Methods: []FuncInfo{{Name: "method1"}, {Name: "method2"}}},
		},
		Functions: []FuncInfo{{Name: "helper"}},
	}
	abstract := SkeletonToAbstract(sk, "test.py")
	if !strings.Contains(abstract, "MyClass") {
		t.Errorf("abstract missing class: %s", abstract)
	}
	if !strings.Contains(abstract, "helper") {
		t.Errorf("abstract missing function: %s", abstract)
	}
}

func TestUnsupportedLanguage(t *testing.T) {
	sk := ExtractSkeleton("some content", "test.xyz")
	if sk != nil {
		t.Errorf("expected nil for unknown extension")
	}
}

func TestCSharpExtractor(t *testing.T) {
	src := `
namespace MyApp.Services
{
    public class UserService : IUserService
    {
        public async Task<User> GetUser(string id)
        {
            return await _db.Find(id);
        }
    }
}
`
	sk := ExtractSkeleton(src, "UserService.cs")
	if sk == nil {
		t.Fatal("nil skeleton")
	}
	if !strings.Contains(sk.Package, "MyApp") {
		t.Errorf("namespace = %s", sk.Package)
	}
	if len(sk.Classes) != 1 {
		t.Errorf("classes = %d", len(sk.Classes))
	}
}
