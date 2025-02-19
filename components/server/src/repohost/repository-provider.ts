/**
 * Copyright (c) 2020 Gitpod GmbH. All rights reserved.
 * Licensed under the GNU Affero General Public License (AGPL).
 * See License-AGPL.txt in the project root for license information.
 */


import { Branch, CommitInfo, Repository, User } from "@gitpod/gitpod-protocol"

export const RepositoryProvider = Symbol('RepositoryProvider');
export interface RepositoryProvider {
    getRepo(user: User, owner: string, repo: string): Promise<Repository>;
    getBranches(user: User, owner: string, repo: string): Promise<Branch[]>;
    getCommitInfo(user: User, owner: string, repo: string, ref: string): Promise<CommitInfo | undefined>
}