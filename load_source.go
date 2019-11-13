package sourcegit

import (
	"fmt"
	"github.com/emptyinterface/sshconfig"
	"gopkg.in/libgit2/git2go.v26"
	"io/ioutil"
	"net/url"
	"os"
	"regexp"
	"github.com/apuigsech/seekret"
	"github.com/apuigsech/seekret/models"
)


var (
	SourceTypeGit = &SourceGit{}
)

const (
	Type = "seekret-source-git"
)

type SourceGit struct{}


type SourceGitLoadOptions struct {
	// commit-files: Include commited file content as object.
	CommitFiles bool
	// commit-messages: Include commit contect as object.
	CommitMessages bool
	// staged-files: Include stateg dile contect as object.
	StagedFiles bool

	// commit-count: Ammount of commits to analise.
	CommitCount int
}

func prepareGitLoadOptions(o seekret.LoadOptions) SourceGitLoadOptions {
	opt := SourceGitLoadOptions{
		CommitFiles: false,
		CommitMessages: false,
		StagedFiles: false,

		CommitCount: 0,
	}

	if commit, ok := o["commit-files"].(bool); ok {
		opt.CommitFiles = commit
	}

	if commitMessages, ok := o["commit-messages"].(bool); ok {
		opt.CommitMessages = commitMessages
	}

	if stagedFiles, ok := o["staged-files"].(bool); ok {
		opt.StagedFiles = stagedFiles
	}

	if commitCount, ok := o["commit-count"].(int); ok {
		opt.CommitCount = commitCount
	}

	return opt
}

func (s *SourceGit) LoadObjects(source string, opta seekret.LoadOptions) ([]models.Object, error) {
	var objectList []models.Object

	opt := prepareGitLoadOptions(opta)

	repo, err := openGitRepo(source)
	if err != nil {
		return nil, err
	}

	if opt.CommitFiles && opt.CommitMessages {
		objectListCommit,err := objectsFromCommit(repo, opt.CommitFiles, opt.CommitMessages, opt.CommitCount)
		if err != nil {
			return nil,err
		}
		objectList = append(objectList, objectListCommit...)
	}

	if opt.StagedFiles {
		objectListStagedFiles,err := objectsFromStagedFiles(repo)
		if err != nil {
			return nil,err
		}
		objectList = append(objectList, objectListStagedFiles...)
	}

	return objectList, nil
}

func objectsFromCommit(repo *git.Repository, commitFiles bool, commitMessages bool, count int) ([]models.Object, error) {
	var objectList []models.Object

	walk, err := repo.Walk()
	if err != nil {
		return nil, err
	}

	if count > 0 {
		err := walk.PushRange(fmt.Sprintf("HEAD~%d..HEAD", count))
		if err != nil {
			err := walk.PushHead()
			if err != nil {
				return nil,err
			}
		}
	} else {
		err := walk.PushHead()
		if err != nil {
			return nil,err
		}
	}
	walk.Sorting(git.SortTime)

	err = walk.Iterate(func(commit *git.Commit) bool {
		tree, err := commit.Tree()
		if err != nil {
			fmt.Println(err)
		}

		if commitMessages {
			o := models.NewObject(fmt.Sprintf("commit-%s", commit.Id()), Type, "commit-message", []byte(commit.Message()))
			o.SetMetadata("commit", commit.Id().String(), models.MetadataAttributes{})
			objectList = append(objectList, *o)
		}


		if commitFiles {
			// TODO: what to return?
			tree.Walk(func(base string, tentry *git.TreeEntry) int {
				if tentry.Type == git.ObjectBlob {
					blob, err := repo.LookupBlob(tentry.Id)
					if err != nil {
						return 0
					}	

					o := models.NewObject(fmt.Sprintf("%s%s", base, tentry.Name), Type, "file-content", blob.Contents())

					o.SetMetadata("commit", commit.Id().String(), models.MetadataAttributes{})
					o.SetMetadata("uniq-id", tentry.Id.String(), models.MetadataAttributes{
						PrimaryKey: true,
					})
					objectList = append(objectList, *o)
				}

				return 0
			})
		}

		return true
	})

	if err != nil {
		return nil, err
	}

	return objectList, nil
}


func objectsFromStagedFiles(repo *git.Repository) ([]models.Object, error) {
	var objectList []models.Object

	index, err := repo.Index()
	if err != nil {
		return nil,err
	}

	for i := 0; i < int(index.EntryCount()); i++ {

		entry, err := index.EntryByIndex(uint(i))
		if err != nil {
			return nil,err
		}

		status, err := repo.StatusFile(entry.Path)
		if err != nil {
			return nil,err
		}

		if status != git.StatusCurrent {
			blob, err := repo.LookupBlob(entry.Id)
			if err != nil {
				return nil,err
			}

			o := models.NewObject(entry.Path, Type, "file-content", blob.Contents())

			// TODO: Type of staged.
			o.SetMetadata("status", "staged", models.MetadataAttributes{})
			objectList = append(objectList, *o)
		}
	}

	return objectList,nil
}


func credentialsCallback(gitUri string, username string, allowedTypes git.CredType) (git.ErrorCode, *git.Cred) {
	sshConfigFile := os.ExpandEnv("$HOME/.ssh/config")

	fh, err := os.Open(sshConfigFile)
	if err != nil {
		panic(err)
	}

	c, err := sshconfig.Parse(fh)
	if err != nil {
		panic(err)
	}

	fh.Close()

	u, err := url.Parse(gitUri)
	if err != nil {
		panic(err)
	}

	host := c.FindByHostname(u.Host)
	idFile := host.GetParam("IdentityFile").Value()
	idFilePub := idFile + ".pub"

	ret, cred := git.NewCredSshKey("git", idFilePub, idFile, "")

	return git.ErrorCode(ret), &cred
}

func certificateCheckCallback(cert *git.Certificate, valid bool, hostname string) git.ErrorCode {
	return 0
}

func normalizeGitUri(source string) (string, bool) {
	var gitUri string

	gitregexp := regexp.MustCompile("^(?:(https?|git|ssh)://|(git@))([^:|/]+)(?:/|:)([^/]+)/([^/\\.]+)(.git)$")
	u := gitregexp.FindStringSubmatch(source)

	if len(u) == 0 {
		return source, false
	}

	var proto string
	if u[1] == "http" || u[1] == "https" || u[1] == "ssh" || u[1] == "git" {
		proto = u[1]
	} else {
		proto = "ssh"
	}

	gitUri = fmt.Sprintf("%s://%s%s/%s/%s%s", proto, u[2], u[3], u[4], u[5], u[6])

	return gitUri, true
}

func openGitRepo(source string) (*git.Repository, error) {
	var repo *git.Repository

	gitUri, remote := normalizeGitUri(source)

	if remote {
		return openGitRepoRemote(gitUri)
	} else {
		return openGitRepoLocal(source)
	}

	return repo, nil
}

func openGitRepoRemote(gitUri string) (*git.Repository, error) {
	var repo *git.Repository
	var err error

	tmpdir, err := ioutil.TempDir("", "seekret")
	if err != nil {
		return nil, err
	}

	repo, err = git.Clone(gitUri, tmpdir, &git.CloneOptions{
		FetchOptions: &git.FetchOptions{
			RemoteCallbacks: git.RemoteCallbacks{
				CredentialsCallback:      credentialsCallback,
				CertificateCheckCallback: certificateCheckCallback,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return repo, nil
}

func openGitRepoLocal(source string) (*git.Repository, error) {
	repo, err := git.OpenRepositoryExtended(source, git.RepositoryOpenCrossFs, "")
	if  err != nil{
		return nil, err
	}

	return repo, nil
}
