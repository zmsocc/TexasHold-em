# MySQL 数据库配置说明

## 1. 安装 MySQL

确保你已经安装了 MySQL 数据库服务器。

## 2. 创建数据库

连接到 MySQL 并创建数据库：

```sql
CREATE DATABASE texas_poker CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

## 3. 修改数据库连接配置

编辑 `database.go` 文件，修改第 15 行的数据库连接字符串：

```go
dsn := "root:password@tcp(localhost:3306)/texas_poker?charset=utf8mb4&parseTime=True&loc=Local"
```

将 `root:password` 替换为你的 MySQL 用户名和密码。

## 4. 安装依赖

```bash
go mod tidy
```

## 5. 运行程序

```bash
go build -o trae-puke-web.exe
.\trae-puke-web.exe
```

## 6. 如果没有 MySQL

如果没有安装 MySQL，程序会自动使用内存存储（数据在程序重启后会丢失）。
