package test

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gruntwork-io/terratest/modules/gcp"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/gruntwork-io/terratest/modules/test-structure"
	"github.com/stretchr/testify/assert"
	"log"
	"path/filepath"
	"strings"
	"testing"
)

const DB_NAME = "testdb"
const DB_USER = "testuser"
const DB_PASS = "testpassword"

// TODO: try to actually connect to the RDS DBs and check they are working
func TestCloudSQLMySql(t *testing.T) {
	t.Parallel()

	//os.Setenv("SKIP_bootstrap", "true")
	//os.Setenv("SKIP_deploy", "true")
	//os.Setenv("SKIP_validate_outputs", "true")
	//os.Setenv("SKIP_sql_tests", "true")
	//os.Setenv("SKIP_teardown", "true")

	_examplesDir := test_structure.CopyTerraformFolderToTemp(t, "../", "examples")
	exampleDir := filepath.Join(_examplesDir, "cloud-sql-mysql")
	rootDir := filepath.Join(_examplesDir, "../")

	test_structure.RunTestStage(t, "bootstrap", func() {
		uniqueId := strings.ToLower(random.UniqueId())
		instanceName := fmt.Sprintf("mysql-test-%s", uniqueId)
		projectId := gcp.GetGoogleProjectIDFromEnvVar(t)
		region := getRandomRegion(t, projectId)

		test_structure.SaveString(t, exampleDir, "instance-name", instanceName)
		test_structure.SaveString(t, exampleDir, "region", region)
		test_structure.SaveString(t, exampleDir, "project-id", projectId)
	})

	// At the end of the test, run `terraform destroy` to clean up any resources that were created
	defer test_structure.RunTestStage(t, "teardown", func() {
		terraformOptions := test_structure.LoadTerraformOptions(t, exampleDir)
		terraform.Destroy(t, terraformOptions)
	})

	test_structure.RunTestStage(t, "deploy", func() {
		region := test_structure.LoadString(t, exampleDir, "region")
		projectId := test_structure.LoadString(t, exampleDir, "project-id")
		instanceName := test_structure.LoadString(t, exampleDir, "instance-name")
		terraformOptions := createTerratestOptionsForMySql(projectId, region, instanceName, rootDir)
		test_structure.SaveTerraformOptions(t, exampleDir, terraformOptions)

		terraform.InitAndApply(t, terraformOptions)
	})

	test_structure.RunTestStage(t, "validate_outputs", func() {
		terraformOptions := test_structure.LoadTerraformOptions(t, exampleDir)
		instanceName := test_structure.LoadString(t, exampleDir, "instance-name")

		region := test_structure.LoadString(t, exampleDir, "region")
		projectId := test_structure.LoadString(t, exampleDir, "project-id")

		expectedDBConn := fmt.Sprintf("%s:%s:%s", projectId, region, instanceName)

		assert.Equal(t, instanceName, terraform.Output(t, terraformOptions, "instance_name"))
		assert.Equal(t, "testdb", terraform.Output(t, terraformOptions, "db_name"))
		assert.Equal(t, expectedDBConn, terraform.Output(t, terraformOptions, "proxy_connection"))
	})

	test_structure.RunTestStage(t, "sql_tests", func() {
		terraformOptions := test_structure.LoadTerraformOptions(t, exampleDir)

		publicIp := terraform.Output(t, terraformOptions, "public_ip")

		connectionString := fmt.Sprintf("%s:%s@tcp(%s:3306)/%s", DB_USER, DB_PASS, publicIp, DB_NAME)

		// Does not actually open up the connection - just returns a DB ref
		logger.Logf(t, "Connecting to: %s", publicIp)
		db, err := sql.Open("mysql",
			connectionString)

		if err != nil {
			t.Fatalf("Failed to open DB connection: %v", err)
		}

		// Make sure we clean up properly
		defer db.Close()

		// Run ping to actually test the connection
		logger.Log(t, "Ping the DB")
		if err = db.Ping(); err != nil {
			t.Fatalf("Failed to ping DB: %v", err)
		}

		// Create table if not exists
		logger.Logf(t, "Create table: %s", MYSQL_CREATE_TEST_TABLE_WITH_AUTO_INCREMENT_STATEMENT)
		if _, err = db.Exec(MYSQL_CREATE_TEST_TABLE_WITH_AUTO_INCREMENT_STATEMENT); err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		// Clean up
		logger.Logf(t, "Empty table: %s", MYSQL_EMPTY_TEST_TABLE_STATEMENT)
		if _, err = db.Exec(MYSQL_EMPTY_TEST_TABLE_STATEMENT); err != nil {
			t.Fatalf("Failed to clean up table: %v", err)
		}

		// Insert data to check that our auto-increment flags worked
		logger.Logf(t, "Insert data: %s", MYSQL_INSERT_TEST_ROW)
		stmt, err := db.Prepare(MYSQL_INSERT_TEST_ROW)
		if err != nil {
			t.Fatalf("Failed to prepare statement: %v", err)
		}

		// Execute the statement
		res, err := stmt.Exec("Grunt")
		if err != nil {
			t.Fatalf("Failed to execute statement: %v", err)
		}

		// Get the last insert id
		lastId, err := res.LastInsertId()
		if err != nil {
			log.Fatal(err)
		}
		// Since we set the auto increment to 5, modulus should always be 0
		assert.Equal(t, int64(0), int64(lastId%5))
	})
}

func createTerratestOptionsForMySql(projectId string, region string, instanceName string, repoPath string) *terraform.Options {

	terratestOptions := &terraform.Options{
		// The path to where your Terraform code is located
		TerraformDir: filepath.Join(repoPath, "examples", "cloud-sql-mysql"),
		Vars: map[string]interface{}{
			"region":          region,
			"project":         projectId,
			"name":            instanceName,
			"mysql_version":   "MYSQL_5_7",
			"db_name":         DB_NAME,
			"master_username": DB_USER,
			"master_password": DB_PASS,
		},
	}

	return terratestOptions
}
