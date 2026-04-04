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

func TestGoExtractor(t *testing.T) {
	src := `package main

import (
	"fmt"
	"os"
)

const Version = "1.0"

type Server struct {
	addr string
}

type Handler interface {
	Handle()
}

func NewServer(addr string) *Server {
	return &Server{addr: addr}
}

func (s *Server) Start() error {
	fmt.Println("starting", s.addr)
	return nil
}

func (s *Server) Stop() {
	os.Exit(0)
}
`
	sk := ExtractSkeleton(src, "main.go")
	if sk == nil {
		t.Fatal("nil skeleton")
	}
	if sk.Language != "go" {
		t.Errorf("language = %s", sk.Language)
	}
	if sk.Package != "package main" {
		t.Errorf("package = %s", sk.Package)
	}
	if len(sk.Imports) < 2 {
		t.Errorf("imports = %d, want >= 2", len(sk.Imports))
	}
	if len(sk.Classes) != 1 {
		t.Errorf("structs = %d, want 1", len(sk.Classes))
	}
	if sk.Classes[0].Name != "Server" {
		t.Errorf("struct name = %s", sk.Classes[0].Name)
	}
	if len(sk.Interfaces) != 1 || sk.Interfaces[0] != "Handler" {
		t.Errorf("interfaces = %v", sk.Interfaces)
	}
	if len(sk.Constants) < 1 {
		t.Error("expected at least 1 constant")
	}

	hasMethod := false
	hasFunc := false
	for _, f := range sk.Functions {
		if f.Name == "Start" && f.Receiver == "Server" {
			hasMethod = true
		}
		if f.Name == "NewServer" && f.Receiver == "" {
			hasFunc = true
		}
	}
	if !hasMethod {
		t.Error("missing Start method on Server")
	}
	if !hasFunc {
		t.Error("missing NewServer function")
	}

	abs := SkeletonToAbstract(sk, "main.go")
	if !strings.Contains(abs, "Server") {
		t.Error("abstract should mention Server")
	}
}

func TestPhpExtractor(t *testing.T) {
	src := `<?php

namespace App\Controllers;

use App\Models\User;
use App\Services\AuthService;

interface Authenticatable {
    public function authenticate(): bool;
}

trait HasTimestamps {
    public function getCreatedAt(): string {
        return $this->created_at;
    }
}

class UserController extends BaseController implements Authenticatable
{
    public function index(): Response
    {
        return $this->json(User::all());
    }

    public static function create(Request $request): Response
    {
        return new Response();
    }

    private function validate(array $data): bool
    {
        return true;
    }
}
`
	sk := ExtractSkeleton(src, "UserController.php")
	if sk == nil {
		t.Fatal("nil skeleton")
	}
	if sk.Language != "php" {
		t.Errorf("language = %s", sk.Language)
	}
	if !strings.Contains(sk.Package, "App\\Controllers") {
		t.Errorf("namespace = %s", sk.Package)
	}
	if len(sk.Imports) < 2 {
		t.Errorf("imports = %d, want >= 2", len(sk.Imports))
	}
	if len(sk.Interfaces) < 1 {
		t.Error("expected at least 1 interface")
	}

	hasClass := false
	for _, c := range sk.Classes {
		if c.Name == "UserController" {
			hasClass = true
			if c.Extends != "BaseController" {
				t.Errorf("extends = %s", c.Extends)
			}
		}
	}
	if !hasClass {
		t.Error("missing UserController class")
	}

	if len(sk.Functions) < 2 {
		t.Errorf("functions = %d, want >= 2", len(sk.Functions))
	}
}

func TestCppExtractor(t *testing.T) {
	src := `#include <iostream>
#include <vector>
#include "myheader.h"

namespace game {

class Player : public Entity {
public:
    void move(float dx, float dy);
    int getHealth() const;
};

enum class Direction {
    Up, Down, Left, Right
};

int main(int argc, char** argv) {
    Player p;
    p.move(1.0, 2.0);
    return 0;
}

void Player::move(float dx, float dy) {
    x += dx;
    y += dy;
}

}
`
	sk := ExtractSkeleton(src, "game.cpp")
	if sk == nil {
		t.Fatal("nil skeleton")
	}
	if sk.Language != "cpp" {
		t.Errorf("language = %s", sk.Language)
	}
	if !strings.Contains(sk.Package, "game") {
		t.Errorf("namespace = %s", sk.Package)
	}
	if len(sk.Imports) < 3 {
		t.Errorf("includes = %d, want >= 3", len(sk.Imports))
	}

	hasPlayer := false
	for _, c := range sk.Classes {
		if c.Name == "Player" {
			hasPlayer = true
			if c.Extends != "Entity" {
				t.Errorf("extends = %s", c.Extends)
			}
		}
	}
	if !hasPlayer {
		t.Error("missing Player class")
	}

	if len(sk.Constants) < 1 {
		t.Error("expected Direction enum in constants")
	}

	hasMain := false
	for _, f := range sk.Functions {
		if f.Name == "main" {
			hasMain = true
		}
	}
	if !hasMain {
		t.Error("missing main function")
	}
}

func TestCExtractor(t *testing.T) {
	src := `#include <stdio.h>
#include <stdlib.h>

struct Point {
    int x;
    int y;
};

int add(int a, int b) {
    return a + b;
}

void print_point(struct Point p) {
    printf("(%d, %d)\n", p.x, p.y);
}
`
	sk := ExtractSkeleton(src, "utils.c")
	if sk == nil {
		t.Fatal("nil skeleton")
	}
	if sk.Language != "c" {
		t.Errorf("language = %s", sk.Language)
	}
	if len(sk.Imports) < 2 {
		t.Errorf("includes = %d", len(sk.Imports))
	}
	if len(sk.Classes) < 1 {
		t.Error("expected Point struct")
	}

	hasAdd := false
	for _, f := range sk.Functions {
		if f.Name == "add" {
			hasAdd = true
		}
	}
	if !hasAdd {
		t.Error("missing add function")
	}
}
